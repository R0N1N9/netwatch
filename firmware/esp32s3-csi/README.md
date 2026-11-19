# NetWatch SONAR node (ESP32-S3)

Streams Wi-Fi CSI amplitudes to NetWatch for presence sensing.

**Status: not yet compiled or tested on hardware.** It was written against the
ESP-IDF v5 API before the boards arrived. Expect small fixes on first build.

1. Edit the defines at the top of `main/csi_node.c` (SSID, password, a unique
   `NODE_NAME`, and the IP of the machine running NetWatch).
2. Build and flash with ESP-IDF v5.x:

   ```bash
   idf.py set-target esp32s3
   ```

   ```bash
   idf.py build flash monitor
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
