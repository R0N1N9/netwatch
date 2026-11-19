// NetWatch ambient threat light for an ESP32 with a WS2812 ("NeoPixel") strip.
//
// Joins your Wi-Fi and listens on UDP for one small datagram a second from
// NetWatch:
//   {"r":255,"g":30,"b":20,"pulse":"breathe","why":"2 critical alert(s) unopened"}
// and shows that colour: steady, breathing, or flashing. If NetWatch goes
// quiet for 10 seconds the strip dims to a faint white so you can tell the
// light has lost its source rather than that all is well.
//
// Set the light's address (this board's IP and LIGHT_PORT) in the REFLEX deck.
//
// The board's own RGB light shows the same colour, so it works without a
// strip. At power-up everything flashes red, green, blue once as a self-test.
//
// STATUS: compiles with ESP-IDF v5.3.2 and the espressif/led_strip component
// (v2.x) in CI, but has NOT yet run on hardware.

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_partition.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "led_strip.h"
#include "lwip/sockets.h"
#include "nvs_flash.h"

// ---- Wi-Fi details are only needed here if you flash by hand; NetWatch writes them when it flashes the board ----
#define WIFI_SSID     "your-ssid"
#define WIFI_PASSWORD "your-password"
#define LIGHT_PORT    5577
#define STRIP_GPIO    8   // data pin of the strip
#define STRIP_LEDS    30

static const char *TAG = "nw-light";
static led_strip_handle_t s_strip;
// The board's own RGB light mirrors the strip, so a bare board works as a small
// threat light. DevKitC-1 style boards have it on GPIO48 (v1.0) or GPIO38 (v1.1).
#define ONBOARD_GPIO_A 48
#define ONBOARD_GPIO_B 38
static led_strip_handle_t s_onboard[2];
static volatile uint8_t s_r = 20, s_g = 20, s_b = 20;
static volatile int s_pulse = 0;            // 0 steady, 1 breathe, 2 flash
static volatile int64_t s_last_us = 0;

// ---- settings written by NetWatch when it flashes the board ----
// NetWatch stores a small text block in the "nwcfg" partition:
//   NWCFG1\nssid=...\npass=...\nhost=...\nport=5566\nname=...\n
// If it is not there (the board was flashed by hand), the defines above are used.
static char s_cfg_ssid[33], s_cfg_pass[65], s_cfg_host[40], s_cfg_name[24];
static int s_cfg_port;

static void cfg_value(const char *blob, const char *key, char *out, size_t cap) {
    char find[16];
    snprintf(find, sizeof(find), "\n%s=", key);
    const char *p = strstr(blob, find);
    if (!p) {
        return;
    }
    p += strlen(find);
    size_t n = strcspn(p, "\n");
    if (n >= cap) {
        n = cap - 1;
    }
    memcpy(out, p, n);
    out[n] = 0;
}

static void load_config(void) {
    static char blob[513];
    char port[8] = "";
    const esp_partition_t *part = esp_partition_find_first(ESP_PARTITION_TYPE_DATA, ESP_PARTITION_SUBTYPE_ANY, "nwcfg");
    if (!part || esp_partition_read(part, 0, blob, 512) != ESP_OK || strncmp(blob, "NWCFG1\n", 7) != 0) {
        ESP_LOGW(TAG, "no settings from NetWatch on this board; using the ones compiled in");
        return;
    }
    blob[512] = 0;
    cfg_value(blob + 6, "ssid", s_cfg_ssid, sizeof(s_cfg_ssid));
    cfg_value(blob + 6, "pass", s_cfg_pass, sizeof(s_cfg_pass));
    cfg_value(blob + 6, "host", s_cfg_host, sizeof(s_cfg_host));
    cfg_value(blob + 6, "name", s_cfg_name, sizeof(s_cfg_name));
    cfg_value(blob + 6, "port", port, sizeof(port));
    if (atoi(port) > 0) {
        s_cfg_port = atoi(port);
    }
    ESP_LOGI(TAG, "settings from NetWatch: network \"%s\"", s_cfg_ssid);
}

// Reads the integer after "key": in a small JSON object. No allocation, no recursion.
static int field(const char *json, const char *key, int fallback) {
    const char *p = strstr(json, key);
    if (!p) return fallback;
    p = strchr(p, ':');
    if (!p) return fallback;
    int v = atoi(p + 1);
    return v < 0 ? 0 : v > 255 ? 255 : v;
}

static void udp_task(void *arg) {
    int sock = socket(AF_INET, SOCK_DGRAM, IPPROTO_IP);
    struct sockaddr_in me = {.sin_family = AF_INET, .sin_port = htons(LIGHT_PORT), .sin_addr.s_addr = htonl(INADDR_ANY)}; // the light always listens on LIGHT_PORT
    bind(sock, (struct sockaddr *)&me, sizeof(me));
    static char buf[256];
    for (;;) {
        int n = recvfrom(sock, buf, sizeof(buf) - 1, 0, NULL, NULL);
        if (n <= 0) continue;
        buf[n] = 0;
        s_r = (uint8_t)field(buf, "\"r\"", s_r);
        s_g = (uint8_t)field(buf, "\"g\"", s_g);
        s_b = (uint8_t)field(buf, "\"b\"", s_b);
        s_pulse = strstr(buf, "\"flash\"") ? 2 : strstr(buf, "\"breathe\"") ? 1 : 0;
        s_last_us = esp_timer_get_time();
    }
}

static void show(uint8_t r, uint8_t g, uint8_t b, float level) {
    for (int i = 0; i < STRIP_LEDS; i++) {
        led_strip_set_pixel(s_strip, i, (uint32_t)(r * level), (uint32_t)(g * level), (uint32_t)(b * level));
    }
    led_strip_refresh(s_strip);
    for (int i = 0; i < 2; i++) {
        if (s_onboard[i]) {
            led_strip_set_pixel(s_onboard[i], 0, (uint32_t)(r * level * 0.5f), (uint32_t)(g * level * 0.5f), (uint32_t)(b * level * 0.5f));
            led_strip_refresh(s_onboard[i]);
        }
    }
}

static void draw_task(void *arg) {
    // Power-up self-test: red, green, blue. If you never see this on the board
    // itself, its RGB light is not wired to GPIO48/38 (some boards need a solder
    // jumper marked "RGB" bridged).
    show(255, 0, 0, 0.5f);
    vTaskDelay(pdMS_TO_TICKS(350));
    show(0, 255, 0, 0.5f);
    vTaskDelay(pdMS_TO_TICKS(350));
    show(0, 0, 255, 0.5f);
    vTaskDelay(pdMS_TO_TICKS(350));
    for (;;) {
        int64_t now = esp_timer_get_time();
        float t = now / 1e6f, level = 0.6f;
        if (s_last_us == 0 || now - s_last_us > 10000000) {
            show(40, 40, 40, 0.15f); // no word from NetWatch
        } else {
            if (s_pulse == 1) level = 0.15f + 0.45f * (0.5f + 0.5f * sinf(t * 2.4f));
            if (s_pulse == 2) level = fmodf(t, 0.5f) < 0.25f ? 0.9f : 0.05f;
            show(s_r, s_g, s_b, level);
        }
        vTaskDelay(pdMS_TO_TICKS(40));
    }
}

static void on_event(void *arg, esp_event_base_t base, int32_t id, void *data) {
    if (base == WIFI_EVENT && (id == WIFI_EVENT_STA_START || id == WIFI_EVENT_STA_DISCONNECTED)) {
        esp_wifi_connect();
    } else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        ip_event_got_ip_t *e = (ip_event_got_ip_t *)data;
        ESP_LOGI(TAG, "light is at " IPSTR ":%d — put that in the REFLEX deck", IP2STR(&e->ip_info.ip), LIGHT_PORT);
    }
}

void app_main(void) {
    ESP_ERROR_CHECK(nvs_flash_init());
    snprintf(s_cfg_ssid, sizeof(s_cfg_ssid), "%s", WIFI_SSID);
    snprintf(s_cfg_pass, sizeof(s_cfg_pass), "%s", WIFI_PASSWORD);
    s_cfg_port = LIGHT_PORT;
    load_config();
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    esp_netif_create_default_wifi_sta();
    wifi_init_config_t init = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&init));
    ESP_ERROR_CHECK(esp_event_handler_register(WIFI_EVENT, ESP_EVENT_ANY_ID, on_event, NULL));
    ESP_ERROR_CHECK(esp_event_handler_register(IP_EVENT, IP_EVENT_STA_GOT_IP, on_event, NULL));
    wifi_config_t wc = {0};
    // These two fields are fixed-size byte arrays, not C strings: a full-length name has no terminator.
    memcpy(wc.sta.ssid, s_cfg_ssid, strnlen(s_cfg_ssid, sizeof(wc.sta.ssid)));
    memcpy(wc.sta.password, s_cfg_pass, strnlen(s_cfg_pass, sizeof(wc.sta.password)));
    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &wc));
    ESP_ERROR_CHECK(esp_wifi_start());

    led_strip_config_t strip = {.strip_gpio_num = STRIP_GPIO, .max_leds = STRIP_LEDS, .led_pixel_format = LED_PIXEL_FORMAT_GRB, .led_model = LED_MODEL_WS2812};
    led_strip_rmt_config_t rmt = {.resolution_hz = 10 * 1000 * 1000};
    ESP_ERROR_CHECK(led_strip_new_rmt_device(&strip, &rmt, &s_strip));
    const int pins[2] = {ONBOARD_GPIO_A, ONBOARD_GPIO_B};
    for (int i = 0; i < 2; i++) {
        led_strip_config_t one = {.strip_gpio_num = pins[i], .max_leds = 1, .led_pixel_format = LED_PIXEL_FORMAT_GRB, .led_model = LED_MODEL_WS2812};
        if (led_strip_new_rmt_device(&one, &rmt, &s_onboard[i]) != ESP_OK) {
            s_onboard[i] = NULL;
        }
    }

    xTaskCreate(udp_task, "light_udp", 4096, NULL, 5, NULL);
    xTaskCreate(draw_task, "light_draw", 4096, NULL, 4, NULL);
}
