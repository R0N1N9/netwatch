# NetWatch SONAR node (ESP32-S3)

Streams Wi-Fi CSI amplitudes to NetWatch for presence sensing.

**Status: compiles (ESP-IDF v5.3.2, built in CI) but not yet run on a board.**
The easy way to flash it is from NetWatch itself: SONAR → ＋ ADD A NODE (see
the guide). The steps below are for building and flashing by hand.

1. Edit the defines at the top of `main/csi_node.c`: SSID, password, and the
   IP of the machine running NetWatch. Leave `NODE_NAME_FIXED` empty and each
   board names itself after its hardware address (`node-a1b2`), so the same
   build goes onto every board.
2. Build once with ESP-IDF v5.x, then flash each board in turn:

   ```bash
   idf.py set-target esp32s3
   ```

   ```bash
   idf.py build
   ```

   ```bash
   idf.py -p /dev/ttyACM0 flash monitor
   ```

3. Start the console so it listens for frames on your LAN:

   ```bash
   netwatch gui --sonar-udp 0.0.0.0:5566
   ```

4. Open the SONAR deck and keep the room still for about five seconds while
   each node calibrates.

Placement: each node senses the path between itself and the access point, so
put nodes where the line to the router crosses the space you care about.
The frame format is one JSON datagram per sample:
`{"node":"node-A","rssi":-51,"amp":[...]}`.
