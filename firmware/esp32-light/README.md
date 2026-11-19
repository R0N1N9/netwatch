# NetWatch ambient threat light

An ESP32 driving a WS2812 LED strip that shows the state of your network as a
colour in the room: green when quiet, amber for warnings, red and breathing
for unopened critical alerts, flashing when one has just arrived, blue while
SONAR is armed.

**Status: compiles (ESP-IDF v5.3.2, `led_strip` v2, built in CI) but not yet
run on hardware.** NetWatch can flash it for you: SONAR → ＋ ADD A NODE →
THREAT LIGHT, which also writes your Wi-Fi details.

1. Edit `main/light.c`: Wi-Fi name and password, the strip's data pin and LED count.
2. `idf.py set-target esp32s3` (or your board), then `idf.py build flash monitor`.
3. The board logs its address. Enter it in the REFLEX deck as `ip:5577` and press SET, then TEST.

No strip? The board's own RGB light shows the same colour (if it is wired to
GPIO48 or GPIO38). At power-up everything flashes red, green, blue once.

NetWatch sends one small UDP datagram a second; nothing is sent back. If
NetWatch stops, the strip dims to a faint white after ten seconds.
