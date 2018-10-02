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
