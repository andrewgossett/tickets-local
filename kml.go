package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

const maxKMLBytes = 5 << 20

type xmlNode struct {
	XMLName  xml.Name
	Text     string    `xml:",chardata"`
	Children []xmlNode `xml:",any"`
}

func parseKML(reader io.Reader, fileName string) (MapOverlayInput, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxKMLBytes+1))
	if err != nil {
		return MapOverlayInput{}, validationError{"could not read the KML file"}
	}
	if len(data) > maxKMLBytes {
		return MapOverlayInput{}, validationError{"KML file must be 5 MB or smaller"}
	}
	if bytes.Contains(bytes.ToUpper(data), []byte("<!DOCTYPE")) {
		return MapOverlayInput{}, validationError{"KML files with a document type declaration are not supported"}
	}

	var root xmlNode
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	if err := decoder.Decode(&root); err != nil {
		return MapOverlayInput{}, validationError{"KML file is not valid XML"}
	}

	overlayName := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	if document := firstDescendant(root, "Document"); document != nil {
		if name := directChildText(*document, "name"); name != "" {
			overlayName = name
		}
	}
	if clean(overlayName) == "" {
		overlayName = "Map overlay"
	}

	features := make([]OverlayFeature, 0)
	visitNodes(root, func(node xmlNode) {
		if node.XMLName.Local != "Placemark" {
			return
		}
		name := directChildText(node, "name")
		collectGeometries(node, name, &features)
	})
	input := MapOverlayInput{
		Name:     abbreviate(clean(overlayName), 160),
		FileName: abbreviate(clean(filepath.Base(fileName)), 255),
		Color:    "#A78BFA",
		Visible:  true,
		Features: features,
	}
	if err := validateOverlayInput(input); err != nil {
		return MapOverlayInput{}, err
	}
	return input, nil
}

func collectGeometries(node xmlNode, featureName string, features *[]OverlayFeature) {
	for _, child := range node.Children {
		switch child.XMLName.Local {
		case "Point":
			if path, err := coordinatesFromNode(child); err == nil && len(path) > 0 {
				*features = append(*features, OverlayFeature{Name: featureName, GeometryType: "point", Paths: [][]MapCoordinate{path}})
			}
		case "LineString":
			if path, err := coordinatesFromNode(child); err == nil && len(path) > 1 {
				*features = append(*features, OverlayFeature{Name: featureName, GeometryType: "line", Paths: [][]MapCoordinate{path}})
			}
		case "Polygon":
			paths := polygonPaths(child)
			if len(paths) > 0 {
				*features = append(*features, OverlayFeature{Name: featureName, GeometryType: "polygon", Paths: paths})
			}
		case "Track":
			path := trackCoordinates(child)
			if len(path) > 1 {
				*features = append(*features, OverlayFeature{Name: featureName, GeometryType: "line", Paths: [][]MapCoordinate{path}})
			}
		default:
			collectGeometries(child, featureName, features)
		}
	}
}

func polygonPaths(node xmlNode) [][]MapCoordinate {
	paths := make([][]MapCoordinate, 0)
	for _, boundaryName := range []string{"outerBoundaryIs", "innerBoundaryIs"} {
		for _, boundary := range descendants(node, boundaryName) {
			ring := firstDescendant(boundary, "LinearRing")
			if ring == nil {
				continue
			}
			path, err := coordinatesFromNode(*ring)
			if err == nil && len(path) >= 3 {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func coordinatesFromNode(node xmlNode) ([]MapCoordinate, error) {
	coordinates := firstDescendant(node, "coordinates")
	if coordinates == nil {
		return nil, fmt.Errorf("coordinates not found")
	}
	return parseCoordinateTuples(coordinates.Text)
}

func parseCoordinateTuples(value string) ([]MapCoordinate, error) {
	fields := strings.Fields(value)
	coordinates := make([]MapCoordinate, 0, len(fields))
	for _, field := range fields {
		parts := strings.Split(field, ",")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid KML coordinate")
		}
		longitude, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		latitude, latErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if lonErr != nil || latErr != nil || !validCoordinates(&latitude, &longitude) {
			return nil, fmt.Errorf("invalid KML coordinate")
		}
		coordinates = append(coordinates, MapCoordinate{Latitude: latitude, Longitude: longitude})
	}
	return coordinates, nil
}

func trackCoordinates(node xmlNode) []MapCoordinate {
	result := make([]MapCoordinate, 0)
	for _, coordinate := range descendants(node, "coord") {
		parts := strings.Fields(coordinate.Text)
		if len(parts) < 2 {
			continue
		}
		longitude, lonErr := strconv.ParseFloat(parts[0], 64)
		latitude, latErr := strconv.ParseFloat(parts[1], 64)
		if lonErr == nil && latErr == nil && validCoordinates(&latitude, &longitude) {
			result = append(result, MapCoordinate{Latitude: latitude, Longitude: longitude})
		}
	}
	return result
}

func directChildText(node xmlNode, localName string) string {
	for _, child := range node.Children {
		if child.XMLName.Local == localName {
			return clean(child.Text)
		}
	}
	return ""
}

func firstDescendant(node xmlNode, localName string) *xmlNode {
	for index := range node.Children {
		child := &node.Children[index]
		if child.XMLName.Local == localName {
			return child
		}
		if result := firstDescendant(*child, localName); result != nil {
			return result
		}
	}
	return nil
}

func descendants(node xmlNode, localName string) []xmlNode {
	result := make([]xmlNode, 0)
	visitNodes(node, func(item xmlNode) {
		if item.XMLName.Local == localName {
			result = append(result, item)
		}
	})
	return result
}

func visitNodes(node xmlNode, visit func(xmlNode)) {
	visit(node)
	for _, child := range node.Children {
		visitNodes(child, visit)
	}
}
