

# gomosaic
gomosaic is an mosaic image generator written entirely in Go. In short: It manages an image database and given a query image returns the image composed or "approximated" of images from the database. The methods implemented are mostly based on "On the use of CBIR in image mosaic generation" by Yue Zhang, 2002, University of Alberta.
For more information see the [Wiki](https://github.com/FabianWe/gomosaic/wiki).

## Background
The project originate from a Golang introduction course where I was looking for a project for the participants. Over time I became more and more interested and started working on this project.
## Examples
![Kybfelsen](https://user-images.githubusercontent.com/11533003/46589109-643e8180-caa6-11e8-8997-01b3655487c3.jpg)
Orignal image (Kybfelsen by Freiburg, photo by me) transformed into a mosaic with 70x50 tiles:
![mosaic-lch4-euclid](https://user-images.githubusercontent.com/11533003/46589149-cd25f980-caa6-11e8-9b58-7233ad2b12d4.jpg)
For more examples check the [Wiki's example page](https://github.com/FabianWe/gomosaic/wiki/Examples).

## Installation
If you are a developer and want to play around / use this library just run `go get -u github.com/FabianWe/gomosaic`.
If you just want to use the software please check the [release](https://github.com/FabianWe/gomosaic/releases) page for downloads. Currently binaries are uploaded for linux and windows with am64, if you need something else please contact me or compile for yourself. You should rename the file to plan gomosaic or gomosaic.exe (instead of for example mosaic mosaicamd64.exe).
For usage instructions see [here](https://github.com/FabianWe/gomosaic/wiki/Usage). Currently it can only run in command line mode, I'm planning to write a GUI though.

## Bugs
There are probably bugs in this software, though it worked fine for "useful"
inputs. Please open an issue on github or send an email to fabianwen#posteo.eu
(replace # with @).
## Nice ways to extend the software
Implement color coherence vectors as mentioned in Yue Zhang, 2002. Also the same image gets selected again and again for a query image, especially with big areas of nearly the same color. Implement a mechanism that prevents this.

## License
Copyright 2018 Fabian Wenzelmann

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
### Tird Party Licenses
See [here](https://github.com/FabianWe/gomosaic/wiki/License).
