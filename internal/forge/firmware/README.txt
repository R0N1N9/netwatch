Firmware images for the boards NetWatch can flash, built by the "firmware"
GitHub Actions workflow from firmware/esp32s3-csi (sonar-node.bin) and
firmware/esp32-light (threat-light.bin). Each is one merged image written at
flash offset 0. They are embedded into the netwatch binary at build time.
