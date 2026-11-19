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
// STATUS: written against ESP-IDF v5 and the espressif/led_strip component
// (v2.x API) but NOT yet compiled or run on hardware. Field names in
// led_strip_config_t changed between component versions; expect to adjust.

#include <math.h>
#include <stdlib.h>
#include <string.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "esp_event.h"
#include "esp_log.h"
#include "esp_netif.h"
#include "esp_timer.h"
#include "esp_wifi.h"
#include "led_strip.h"
#include "lwip/sockets.h"
#include "nvs_flash.h"

// ---- edit these ----
#define WIFI_SSID     "your-ssid"
#define WIFI_PASSWORD "your-password"
#define LIGHT_PORT    5577
#define STRIP_GPIO    8   // data pin of the strip
#define STRIP_LEDS    30

static const char *TAG = "nw-light";
static led_strip_handle_t s_strip;
static volatile uint8_t s_r = 20, s_g = 20, s_b = 20;
static volatile int s_pulse = 0;            // 0 steady, 1 breathe, 2 flash
static volatile int64_t s_last_us = 0;

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
    struct sockaddr_in me = {.sin_family = AF_INET, .sin_port = htons(LIGHT_PORT), .sin_addr.s_addr = htonl(INADDR_ANY)};
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
}

static void draw_task(void *arg) {
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
    ESP_ERROR_CHECK(esp_wifi_start());

    led_strip_config_t strip = {.strip_gpio_num = STRIP_GPIO, .max_leds = STRIP_LEDS, .led_pixel_format = LED_PIXEL_FORMAT_GRB, .led_model = LED_MODEL_WS2812};
    led_strip_rmt_config_t rmt = {.resolution_hz = 10 * 1000 * 1000};
    ESP_ERROR_CHECK(led_strip_new_rmt_device(&strip, &rmt, &s_strip));

    xTaskCreate(udp_task, "light_udp", 4096, NULL, 5, NULL);
    xTaskCreate(draw_task, "light_draw", 4096, NULL, 4, NULL);
}
