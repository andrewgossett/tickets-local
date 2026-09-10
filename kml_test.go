package main

import (
	"strings"
	"testing"
)

func TestParseKMLSupportedGeometry(t *testing.T) {
	input, err := parseKML(strings.NewReader(`<?xml version="1.0"?>
<kml xmlns="http://www.opengis.net/kml/2.2" xmlns:gx="http://www.google.com/kml/ext/2.2">
  <Document>
    <name>County Operations</name>
    <Placemark><name>Command</name><Point><coordinates>-86.70,35.90,0</coordinates></Point></Placemark>
    <Placemark><name>Route A</name><LineString><coordinates>-86.70,35.90 -86.71,35.91</coordinates></LineString></Placemark>
    <Placemark><name>Boundary</name><Polygon><outerBoundaryIs><LinearRing><coordinates>-86.7,35.9 -86.8,35.9 -86.8,36.0 -86.7,35.9</coordinates></LinearRing></outerBoundaryIs></Polygon></Placemark>
    <Placemark><name>Track</name><gx:Track><gx:coord>-86.70 35.90 0</gx:coord><gx:coord>-86.71 35.91 0</gx:coord></gx:Track></Placemark>
  </Document>
</kml>`), "operations.kml")
	if err != nil {
		t.Fatal(err)
	}
	if input.Name != "County Operations" || input.FileName != "operations.kml" {
		t.Fatalf("unexpected overlay metadata: %+v", input)
	}
	if len(input.Features) != 4 {
		t.Fatalf("feature count = %d, want 4: %+v", len(input.Features), input.Features)
	}
	gotTypes := []string{input.Features[0].GeometryType, input.Features[1].GeometryType, input.Features[2].GeometryType, input.Features[3].GeometryType}
	if strings.Join(gotTypes, ",") != "point,line,polygon,line" {
		t.Fatalf("unexpected geometry types: %v", gotTypes)
	}
}

func TestParseKMLRejectsUnsafeOrEmptyFiles(t *testing.T) {
	if _, err := parseKML(strings.NewReader(`<!DOCTYPE kml><kml/>`), "unsafe.kml"); err == nil {
		t.Fatal("parseKML accepted a document type declaration")
	}
	if _, err := parseKML(strings.NewReader(`<kml><Document><name>Empty</name></Document></kml>`), "empty.kml"); err == nil {
		t.Fatal("parseKML accepted a file with no supported geometry")
	}
}
