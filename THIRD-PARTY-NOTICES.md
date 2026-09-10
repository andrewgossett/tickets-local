# Third-party software notices

Tickets Local 0.5.8 can bundle the following unmodified components in its
signed macOS package. Complete corresponding upstream source archives,
including copyright and license files, are included under
`Contents/Resources/Third-Party-Source` in the application bundle.

| Component | Version | License | Project |
|---|---:|---|---|
| Dire Wolf | 1.8.1 | GNU GPL 2.0 | <https://github.com/wb2osz/direwolf> |
| gpsd / libgps | 3.27.5 | BSD 2-Clause | <https://gpsd.gitlab.io/gpsd/> |
| Hamlib | 4.7.2 | GNU LGPL 2.1 or later (library) | <https://github.com/Hamlib/Hamlib> |
| HIDAPI | 0.15.0 | BSD 3-Clause option used for this distribution | <https://github.com/libusb/hidapi> |
| PortAudio | 19.7.0 | MIT | <https://www.portaudio.com/> |
| libusb | 1.0.30 | GNU LGPL 2.1 or later | <https://libusb.info/> |

Tickets Local does not modify these libraries. Dynamically linked library
files remain separate in the app bundle so recipients can replace them with
compatible modified versions. No additional restriction is imposed on reverse
engineering for debugging modifications to an LGPL-covered library.

OpenStreetMap map data is © OpenStreetMap contributors and available under the
Open Database License: <https://www.openstreetmap.org/copyright>.

NOAA/National Weather Service and USGS names identify enabled public data
sources and do not imply endorsement.

## Go QR Code encoder

Tickets Local incorporates `github.com/skip2/go-qrcode`, revision
`da1b6568686e`, to create phone-enrollment QR images locally.

Copyright (c) 2014 Tom Harwood

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
the Software, and to permit persons to whom the Software is furnished to do so,
subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
