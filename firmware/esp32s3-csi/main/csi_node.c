// NetWatch SONAR node for ESP32-S3 (ESP-IDF v5.x).
//
// Joins your Wi-Fi, pings the gateway ~25 times a second so the access point
// keeps sending frames back, reads the Channel State Information of those
// frames, and streams the per-subcarrier amplitudes to NetWatch over UDP as
//   {"node":"node-A","rssi":-51,"amp":[12.0,13.4,...],"dist_cm":412}
//
// Once a second it also reports the signal strength of the other devices it
// overhears on the channel, so NetWatch can place network hosts on the map:
//   {"node":"node-A","seen":[{"mac":"aa:bb:cc:dd:ee:ff","rssi":-52}, ...]}
//
// If the access point is an 802.11mc (FTM) responder, the node also measures
// its distance to it every couple of seconds and includes it as dist_cm.
//
// STATUS: written against the ESP-IDF v5 API but NOT yet compiled or run on
// hardware. Treat it as a starting point and expect to fix small things.

#include <math.h>
#include <stdio.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/task.h"

#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "lwip/sockets.h"
#include "nvs_flash.h"
#include "ping/ping_sock.h"

// ---- edit these ----
#define WIFI_SSID      "your-ssid"
#define WIFI_PASSWORD  "your-password"
#define NODE_NAME      "node-A"          // unique per board
#define NETWATCH_HOST  "192.168.1.50"    // machine running: netwatch gui --sonar-udp 0.0.0.0:5566
#define NETWATCH_PORT  5566
#define FRAME_GAP_US   40000             // ~25 frames per second
// --------------------

#define MAX_SUBCARRIERS 64

typedef struct {
    int8_t rssi;
    uint8_t n;
    float amp[MAX_SUBCARRIERS];
} csi_sample_t;

static const char *TAG = "csi_node";
static QueueHandle_t s_queue;
static uint8_t s_ap_bssid[6];
static int64_t s_last_us;
static uint8_t s_ap_channel;
static bool s_ap_ftm;                 // access point advertises FTM responder support
static volatile uint32_t s_dist_cm;   // latest FTM range, 0 = none yet

// Runs in the Wi-Fi task: keep it short, just copy amplitudes into the queue.
static void csi_rx_cb(void *ctx, wifi_csi_info_t *info) {
    if (!info || !info->buf || memcmp(info->mac, s_ap_bssid, 6) != 0) {
        return; // only frames from our access point
    }
    int64_t now = esp_timer_get_time();
    if (now - s_last_us < FRAME_GAP_US) {
        return;
    }
    s_last_us = now;

    csi_sample_t s = {.rssi = info->rx_ctrl.rssi, .n = 0};
    int pairs = info->len / 2; // buf is [imag, real] per subcarrier
    if (pairs > MAX_SUBCARRIERS) {
        pairs = MAX_SUBCARRIERS;
    }
    for (int i = 0; i < pairs; i++) {
        float im = info->buf[2 * i], re = info->buf[2 * i + 1];
        s.amp[s.n++] = sqrtf(im * im + re * re);
    }
    xQueueSend(s_queue, &s, 0);
}

// ---- overheard devices: transmitter address and signal strength ----
#define MAX_SEEN 24
typedef struct {
    uint8_t mac[6];
    int8_t rssi;
    int64_t at_us;
} seen_t;
static seen_t s_seen[MAX_SEEN];
static portMUX_TYPE s_seen_lock = portMUX_INITIALIZER_UNLOCKED;

// Runs in the Wi-Fi task for every frame heard on the channel.
static void sniff_cb(void *buf, wifi_promiscuous_pkt_type_t type) {
    const wifi_promiscuous_pkt_t *pkt = (const wifi_promiscuous_pkt_t *)buf;
    if (pkt->rx_ctrl.sig_len < 24) {
        return;
    }
    const uint8_t *tx = pkt->payload + 10; // 802.11 address 2: the transmitter
    if (memcmp(tx, s_ap_bssid, 6) == 0 || (tx[0] & 0x01)) {
        return; // skip the access point itself and group addresses
    }
    int64_t now = esp_timer_get_time();
    portENTER_CRITICAL(&s_seen_lock);
    int slot = -1, oldest = 0;
    for (int i = 0; i < MAX_SEEN; i++) {
        if (memcmp(s_seen[i].mac, tx, 6) == 0) { slot = i; break; }
        if (s_seen[i].at_us < s_seen[oldest].at_us) { oldest = i; }
    }
    if (slot < 0) { slot = oldest; memcpy(s_seen[slot].mac, tx, 6); s_seen[slot].rssi = pkt->rx_ctrl.rssi; }
    s_seen[slot].rssi = (int8_t)((3 * s_seen[slot].rssi + pkt->rx_ctrl.rssi) / 4);
    s_seen[slot].at_us = now;
    portEXIT_CRITICAL(&s_seen_lock);
}

// Sends the overheard-device table once a second.
static void seen_task(void *arg) {
    int sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    struct sockaddr_in dst = {.sin_family = AF_INET, .sin_port = htons(NETWATCH_PORT)};
    inet_pton(AF_INET, NETWATCH_HOST, &dst.sin_addr);
    static char json[1400];
    static seen_t copy[MAX_SEEN];
    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(1000));
        portENTER_CRITICAL(&s_seen_lock);
        memcpy(copy, s_seen, sizeof(copy));
        portEXIT_CRITICAL(&s_seen_lock);
        int64_t now = esp_timer_get_time();
        int len = snprintf(json, sizeof(json), "{\"node\":\"%s\",\"seen\":[", NODE_NAME), n = 0;
        for (int i = 0; i < MAX_SEEN && len < (int)sizeof(json) - 64; i++) {
            if (copy[i].at_us == 0 || now - copy[i].at_us > 10000000) {
                continue; // empty or not heard for 10 s
            }
            const uint8_t *m = copy[i].mac;
            len += snprintf(json + len, sizeof(json) - len, "%s{\"mac\":\"%02x:%02x:%02x:%02x:%02x:%02x\",\"rssi\":%d}",
                            n++ ? "," : "", m[0], m[1], m[2], m[3], m[4], m[5], copy[i].rssi);
        }
        len += snprintf(json + len, sizeof(json) - len, "]}");
        if (n > 0) {
            sendto(sock, json, len, 0, (struct sockaddr *)&dst, sizeof(dst));
        }
    }
}

static void sender_task(void *arg) {
    int sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    struct sockaddr_in dst = {.sin_family = AF_INET, .sin_port = htons(NETWATCH_PORT)};
    inet_pton(AF_INET, NETWATCH_HOST, &dst.sin_addr);

    static char json[1024];
    csi_sample_t s;
    for (;;) {
        if (xQueueReceive(s_queue, &s, portMAX_DELAY) != pdTRUE) {
            continue;
        }
        int len = snprintf(json, sizeof(json), "{\"node\":\"%s\",\"rssi\":%d,\"amp\":[", NODE_NAME, s.rssi);
        for (int i = 0; i < s.n && len < (int)sizeof(json) - 16; i++) {
            len += snprintf(json + len, sizeof(json) - len, i ? ",%.1f" : "%.1f", s.amp[i]);
        }
        uint32_t dist = s_dist_cm;
        if (dist > 0) {
            len += snprintf(json + len, sizeof(json) - len, "],\"dist_cm\":%u}", (unsigned)dist);
        } else {
            len += snprintf(json + len, sizeof(json) - len, "]}");
        }
        sendto(sock, json, len, 0, (struct sockaddr *)&dst, sizeof(dst));
    }
}

static void start_gateway_ping(esp_ip4_addr_t gw) {
    esp_ping_config_t cfg = ESP_PING_DEFAULT_CONFIG();
    cfg.target_addr.type = IPADDR_TYPE_V4;
    cfg.target_addr.u_addr.ip4.addr = gw.addr;
    cfg.count = ESP_PING_COUNT_INFINITE;
    cfg.interval_ms = FRAME_GAP_US / 1000;
    esp_ping_callbacks_t cbs = {0};
    esp_ping_handle_t ping;
    if (esp_ping_new_session(&cfg, &cbs, &ping) == ESP_OK) {
        esp_ping_start(ping);
    }
}

static void start_csi(void) {
    wifi_ap_record_t ap;
    if (esp_wifi_sta_get_ap_info(&ap) == ESP_OK) {
        memcpy(s_ap_bssid, ap.bssid, 6);
        s_ap_channel = ap.primary;
        s_ap_ftm = ap.ftm_responder;
        ESP_LOGI(TAG, "access point FTM responder: %s", s_ap_ftm ? "yes" : "no (ranging disabled)");
    }
    wifi_csi_config_t cfg = {
        .lltf_en = true,           // legacy long training field: 64 subcarriers
        .htltf_en = false,
        .stbc_htltf2_en = false,
        .ltf_merge_en = true,
        .channel_filter_en = true,
        .manu_scale = false,
        .shift = 0,
    };
    ESP_ERROR_CHECK(esp_wifi_set_csi_config(&cfg));
    ESP_ERROR_CHECK(esp_wifi_set_csi_rx_cb(csi_rx_cb, NULL));
    ESP_ERROR_CHECK(esp_wifi_set_csi(true));

    // Overhear other devices on the channel for host location.
    wifi_promiscuous_filter_t filt = {.filter_mask = WIFI_PROMIS_FILTER_MASK_DATA | WIFI_PROMIS_FILTER_MASK_MGMT};
    esp_wifi_set_promiscuous_filter(&filt);
    esp_wifi_set_promiscuous_rx_cb(sniff_cb);
    esp_wifi_set_promiscuous(true);
    ESP_LOGI(TAG, "CSI streaming to %s:%d as %s", NETWATCH_HOST, NETWATCH_PORT, NODE_NAME);
}

// Ask the access point for a range every two seconds (needs an FTM-capable AP).
static void ftm_task(void *arg) {
    for (;;) {
        vTaskDelay(pdMS_TO_TICKS(2000));
        if (!s_ap_ftm) {
            continue;
        }
        wifi_ftm_initiator_cfg_t cfg = {.frm_count = 16, .burst_period = 2, .channel = s_ap_channel};
        memcpy(cfg.resp_mac, s_ap_bssid, 6);
        esp_wifi_ftm_initiate_session(&cfg);
    }
}

static void on_event(void *arg, esp_event_base_t base, int32_t id, void *data) {
    if (base == WIFI_EVENT && id == WIFI_EVENT_FTM_REPORT) {
        wifi_event_ftm_report_t *ev = (wifi_event_ftm_report_t *)data;
        if (ev->status == FTM_STATUS_SUCCESS) {
            s_dist_cm = ev->dist_est; // centimetres
        }
        // NOTE: the report-entry handling differs between ESP-IDF releases. On
        // some versions you must free(ev->ftm_report_data) here; on newer ones
        // you fetch entries with esp_wifi_ftm_get_report(). Check your version.
        return;
    }
    if (base == WIFI_EVENT && (id == WIFI_EVENT_STA_START || id == WIFI_EVENT_STA_DISCONNECTED)) {
        esp_wifi_connect();
    } else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        ip_event_got_ip_t *ev = (ip_event_got_ip_t *)data;
        start_csi();
        start_gateway_ping(ev->ip_info.gw);
    }
}

void app_main(void) {
    ESP_ERROR_CHECK(nvs_flash_init());
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    esp_netif_create_default_wifi_sta();

    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&init));
    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID, on_event, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, on_event, NULL));

    wifi_config_t wc = {.sta = {.ssid = WIFI_SSID, .password = WIFI_PASSWORD}};
    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &wc));
    ESP_ERROR_CHECK(esp_wifi_set_ps(WIFI_PS_NONE)); // power save would starve the CSI stream

    s_queue = xQueueCreate(8, sizeof(csi_sample_t));
    xTaskCreate(sender_task, "csi_sender", 4096, NULL, 5, NULL);
    xTaskCreate(ftm_task, "ftm_ranger", 3072, NULL, 4, NULL);
    xTaskCreate(seen_task, "seen_sender", 4096, NULL, 4, NULL);
    ESP_ERROR_CHECK(esp_wifi_start());
}
