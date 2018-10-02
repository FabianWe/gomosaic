// Copyright 2018 Fabian Wenzelmann
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package gomosaic provides methods for generating mosaic images given a
// database (= set) of images. It takes a query image and returns a composition
// of the query with images from the database.
//
// Different metrics can be used to find matching images, also the size of
// the tiles in the result is configurable.
//
// It ships with a executable program to generate mosaic images and administrate
// image databases on the filesystem.
package gomosaic
