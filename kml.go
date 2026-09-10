package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const maxKMLBytes = 5 << 20

type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []xmlNode  `xml:",any"`
}

type kmlStyleColors struct {
	point   string
	line    string
	polygon string
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

	styles, styleMaps := collectKMLStyles(root)
	features := make([]OverlayFeature, 0)
	visitNodes(root, func(node xmlNode) {
		if node.XMLName.Local != "Placemark" {
			return
		}
		name := directChildText(node, "name")
		description := placemarkDescription(node)
		style := resolvePlacemarkStyle(node, styles, styleMaps)
		collectGeometries(node, name, description, style, &features)
	})
	input := MapOverlayInput{
		Name:         abbreviate(clean(overlayName), 160),
		FileName:     abbreviate(clean(filepath.Base(fileName)), 255),
		Color:        "#A78BFA",
		UseKMLStyles: slices.ContainsFunc(features, func(feature OverlayFeature) bool { return feature.Color != "" }),
		Visible:      true,
		Features:     features,
	}
	if err := validateOverlayInput(input); err != nil {
		return MapOverlayInput{}, err
	}
	return input, nil
}

func collectGeometries(node xmlNode, featureName, description string, style kmlStyleColors, features *[]OverlayFeature) {
	for _, child := range node.Children {
		switch child.XMLName.Local {
		case "Point":
			if path, err := coordinatesFromNode(child); err == nil && len(path) > 0 {
				*features = append(*features, OverlayFeature{Name: featureName, Description: description, Color: style.point, GeometryType: "point", Paths: [][]MapCoordinate{path}})
			}
		case "LineString":
			if path, err := coordinatesFromNode(child); err == nil && len(path) > 1 {
				*features = append(*features, OverlayFeature{Name: featureName, Description: description, Color: style.line, GeometryType: "line", Paths: [][]MapCoordinate{path}})
			}
		case "Polygon":
			paths := polygonPaths(child)
			if len(paths) > 0 {
				*features = append(*features, OverlayFeature{Name: featureName, Description: description, Color: style.polygon, GeometryType: "polygon", Paths: paths})
			}
		case "Track":
			path := trackCoordinates(child)
			if len(path) > 1 {
				*features = append(*features, OverlayFeature{Name: featureName, Description: description, Color: style.line, GeometryType: "line", Paths: [][]MapCoordinate{path}})
			}
		default:
			collectGeometries(child, featureName, description, style, features)
		}
	}
}

func collectKMLStyles(root xmlNode) (map[string]kmlStyleColors, map[string]string) {
	styles := make(map[string]kmlStyleColors)
	styleMaps := make(map[string]string)
	visitNodes(root, func(node xmlNode) {
		id := attribute(node, "id")
		if id == "" {
			return
		}
		switch node.XMLName.Local {
		case "Style":
			styles[id] = colorsFromStyle(node)
		case "StyleMap":
			for _, pair := range node.Children {
				if pair.XMLName.Local == "Pair" && directChildText(pair, "key") == "normal" {
					styleMaps[id] = strings.TrimPrefix(directChildText(pair, "styleUrl"), "#")
				}
			}
		}
	})
	return styles, styleMaps
}

func resolvePlacemarkStyle(node xmlNode, styles map[string]kmlStyleColors, styleMaps map[string]string) kmlStyleColors {
	for _, child := range node.Children {
		if child.XMLName.Local == "Style" {
			return colorsFromStyle(child)
		}
	}
	styleID := strings.TrimPrefix(directChildText(node, "styleUrl"), "#")
	if mapped := styleMaps[styleID]; mapped != "" {
		styleID = mapped
	}
	return styles[styleID]
}

func colorsFromStyle(node xmlNode) kmlStyleColors {
	return kmlStyleColors{
		point:   styleColor(node, "IconStyle"),
		line:    styleColor(node, "LineStyle"),
		polygon: firstNonEmpty(styleColor(node, "PolyStyle"), styleColor(node, "LineStyle")),
	}
}

func styleColor(node xmlNode, styleName string) string {
	style := firstDescendant(node, styleName)
	if style == nil {
		return ""
	}
	return kmlColor(directChildText(*style, "color"))
}

func kmlColor(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if len(value) == 8 { // KML uses alpha-blue-green-red.
		value = value[6:8] + value[4:6] + value[2:4]
	}
	if len(value) != 6 {
		return ""
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", character) {
			return ""
		}
	}
	return "#" + strings.ToLower(value)
}

func placemarkDescription(node xmlNode) string {
	parts := make([]string, 0, 4)
	if description := directChildText(node, "description"); description != "" {
		parts = append(parts, description)
	}
	for _, data := range descendants(node, "Data") {
		name := attribute(data, "name")
		value := directChildText(data, "value")
		if name != "" && value != "" {
			parts = append(parts, name+": "+value)
		}
	}
	return abbreviate(cleanKMLText(strings.Join(parts, " · ")), 1000)
}

func cleanKMLText(value string) string {
	value = html.UnescapeString(value)
	var output strings.Builder
	inTag := false
	for _, character := range value {
		switch character {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				output.WriteRune(character)
			}
		}
	}
	return strings.Join(strings.Fields(output.String()), " ")
}

func attribute(node xmlNode, localName string) string {
	for _, attribute := range node.Attrs {
		if attribute.Name.Local == localName {
			return clean(attribute.Value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
