package main

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	errNoAPRSPosition   = errors.New("APRS packet does not contain a supported position")
	altitudePattern     = regexp.MustCompile(`/A=([0-9]{6})`)
	weatherFieldPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9])([csgtrpPhb])(-?[0-9]{2,5})`)
)

type APRSPosition struct {
	Callsign      string
	Latitude      float64
	Longitude     float64
	SpeedKnots    *float64
	CourseDegrees *float64
	AltitudeFeet  *float64
	Symbol        string
	Comment       string
	ReceivedAt    time.Time
	Raw           string
	Weather       *APRSWeatherObservation
}

func parseAPRSPacket(line string, receivedAt time.Time) (APRSPosition, error) {
	line = strings.TrimRight(line, "\r\n")
	if len(line) == 0 || len(line) > 510 || strings.HasPrefix(line, "#") {
		return APRSPosition{}, errNoAPRSPosition
	}
	headerEnd := strings.IndexByte(line, ':')
	addressEnd := strings.IndexByte(line, '>')
	if addressEnd < 1 || headerEnd <= addressEnd+1 {
		return APRSPosition{}, fmt.Errorf("invalid TNC2 packet")
	}
	source := strings.ToUpper(strings.TrimSpace(line[:addressEnd]))
	if !validTrackedCallsign(source) {
		return APRSPosition{}, fmt.Errorf("invalid APRS source callsign")
	}
	destination := line[addressEnd+1 : headerEnd]
	if comma := strings.IndexByte(destination, ','); comma >= 0 {
		destination = destination[:comma]
	}
	if hyphen := strings.IndexByte(destination, '-'); hyphen >= 0 {
		destination = destination[:hyphen]
	}
	info := line[headerEnd+1:]
	if info == "" {
		return APRSPosition{}, errNoAPRSPosition
	}

	// Third-party packets carry another complete TNC2 packet after the data type.
	if info[0] == '}' {
		return parseAPRSPacket(info[1:], receivedAt)
	}

	position := APRSPosition{
		Callsign:   source,
		ReceivedAt: receivedAt.UTC(),
		Raw:        line,
	}
	var err error
	switch info[0] {
	case '!', '=':
		err = parseAPRSPositionBody(info[1:], &position)
	case '/', '@':
		if len(info) < 9 {
			return APRSPosition{}, fmt.Errorf("timestamped APRS position is too short")
		}
		err = parseAPRSPositionBody(info[8:], &position)
	case '\'', '`':
		err = parseMicEPosition(destination, info, &position)
	default:
		return APRSPosition{}, errNoAPRSPosition
	}
	if err != nil {
		return APRSPosition{}, err
	}
	if !validCoordinates(&position.Latitude, &position.Longitude) ||
		math.IsNaN(position.Latitude) || math.IsNaN(position.Longitude) {
		return APRSPosition{}, fmt.Errorf("decoded APRS coordinates are invalid")
	}
	position.Weather = parseAPRSWeather(position.Comment, position.Symbol, position.ReceivedAt)
	return position, nil
}

func parseAPRSWeather(comment, symbol string, observedAt time.Time) *APRSWeatherObservation {
	matches := weatherFieldPattern.FindAllStringSubmatch(comment, 16)
	if !strings.HasSuffix(symbol, "_") && len(matches) < 3 {
		return nil
	}
	observation := &APRSWeatherObservation{ObservedAt: observedAt.UTC()}
	fields := 0
	for _, match := range matches {
		value, err := strconv.ParseFloat(match[2], 64)
		if err != nil {
			continue
		}
		switch match[1] {
		case "c":
			if value >= 0 && value <= 360 {
				observation.WindDirection = pointer(value)
				fields++
			}
		case "s":
			if value >= 0 && value <= 300 {
				observation.WindSpeedMPH = pointer(value)
				fields++
			}
		case "g":
			if value >= 0 && value <= 300 {
				observation.WindGustMPH = pointer(value)
				fields++
			}
		case "t":
			if value >= -100 && value <= 160 {
				observation.TemperatureF = pointer(value)
				fields++
			}
		case "r":
			if value >= 0 && value <= 999 {
				scaled := value / 100
				observation.RainLastHourInches = pointer(scaled)
				fields++
			}
		case "p":
			if value >= 0 && value <= 999 {
				scaled := value / 100
				observation.Rain24HoursInches = pointer(scaled)
				fields++
			}
		case "P":
			if value >= 0 && value <= 999 {
				scaled := value / 100
				observation.RainSinceMidnightInches = pointer(scaled)
				fields++
			}
		case "h":
			if value == 0 {
				value = 100
			}
			if value >= 1 && value <= 100 {
				observation.HumidityPercent = pointer(value)
				fields++
			}
		case "b":
			if value >= 8000 && value <= 11000 {
				scaled := value / 10
				observation.PressureMillibars = pointer(scaled)
				fields++
			}
		}
	}
	if fields == 0 {
		return nil
	}
	return observation
}

func parseAPRSPositionBody(body string, position *APRSPosition) error {
	if len(body) >= 19 && (body[7] == 'N' || body[7] == 'S') &&
		(body[17] == 'E' || body[17] == 'W') {
		return parseUncompressedPosition(body, position)
	}
	if len(body) >= 10 {
		return parseCompressedPosition(body, position)
	}
	return fmt.Errorf("APRS position is too short")
}

func parseUncompressedPosition(body string, position *APRSPosition) error {
	latitude, err := parseDegreesMinutes(body[:8], 2, 'N', 'S')
	if err != nil {
		return fmt.Errorf("decode APRS latitude: %w", err)
	}
	longitude, err := parseDegreesMinutes(body[9:18], 3, 'E', 'W')
	if err != nil {
		return fmt.Errorf("decode APRS longitude: %w", err)
	}
	position.Latitude = latitude
	position.Longitude = longitude
	position.Symbol = string([]byte{body[8], body[18]})
	comment := body[19:]
	if len(comment) >= 7 && comment[3] == '/' {
		course, courseErr := strconv.ParseFloat(comment[:3], 64)
		speed, speedErr := strconv.ParseFloat(comment[4:7], 64)
		if courseErr == nil && speedErr == nil && course >= 0 && course <= 360 && speed >= 0 {
			if course == 360 {
				course = 0
			}
			position.CourseDegrees = pointer(course)
			position.SpeedKnots = pointer(speed)
			comment = comment[7:]
		}
	}
	extractAltitude(comment, position)
	position.Comment = strings.TrimSpace(altitudePattern.ReplaceAllString(comment, ""))
	return nil
}

func parseCompressedPosition(body string, position *APRSPosition) error {
	for index := 1; index <= 8; index++ {
		if body[index] < 33 || body[index] > 123 {
			return fmt.Errorf("compressed APRS position contains a non-base91 character")
		}
	}
	latValue := decodeBase91(body[1:5])
	lonValue := decodeBase91(body[5:9])
	position.Latitude = 90 - float64(latValue)/380926
	position.Longitude = -180 + float64(lonValue)/190463
	position.Symbol = string([]byte{body[0], body[9]})
	commentStart := 10
	if len(body) >= 13 && base91Byte(body[10]) && base91Byte(body[11]) && base91Byte(body[12]) {
		commentStart = 13
	}
	comment := body[commentStart:]
	extractAltitude(comment, position)
	position.Comment = strings.TrimSpace(altitudePattern.ReplaceAllString(comment, ""))
	return nil
}

func parseDegreesMinutes(value string, degreeDigits int, positive, negative byte) (float64, error) {
	if len(value) != degreeDigits+6 {
		return 0, fmt.Errorf("wrong coordinate length")
	}
	hemisphere := value[len(value)-1]
	if hemisphere != positive && hemisphere != negative {
		return 0, fmt.Errorf("invalid hemisphere")
	}
	numeric := []byte(value[:len(value)-1])
	// APRS position ambiguity uses spaces; the midpoint of the ambiguous area is
	// the most useful value for a tactical map.
	for index := range numeric {
		if numeric[index] == ' ' {
			numeric[index] = '5'
		}
	}
	degrees, err := strconv.ParseFloat(string(numeric[:degreeDigits]), 64)
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.ParseFloat(string(numeric[degreeDigits:]), 64)
	if err != nil || minutes >= 60 {
		return 0, fmt.Errorf("invalid minutes")
	}
	result := degrees + minutes/60
	if hemisphere == negative {
		result = -result
	}
	return result, nil
}

func parseMicEPosition(destination, info string, position *APRSPosition) error {
	if len(destination) < 6 || len(info) < 9 {
		return fmt.Errorf("Mic-E packet is too short")
	}
	destination = strings.ToUpper(destination[:6])
	digits := make([]int, 6)
	for index, character := range []byte(destination) {
		digit, ok := micEDigit(character)
		if !ok {
			return fmt.Errorf("invalid Mic-E destination encoding")
		}
		digits[index] = digit
	}
	latitude := float64(digits[0]*10+digits[1]) +
		float64(digits[2]*1000+digits[3]*100+digits[4]*10+digits[5])/6000
	if (destination[3] >= '0' && destination[3] <= '9') || destination[3] == 'L' {
		latitude = -latitude
	}

	offset := destination[4] >= 'P' && destination[4] <= 'Z'
	longitudeByte := int(info[1])
	var longitude float64
	switch {
	case offset && longitudeByte >= 118 && longitudeByte <= 127:
		longitude = float64(longitudeByte - 118)
	case !offset && longitudeByte >= 38 && longitudeByte <= 127:
		longitude = float64(longitudeByte - 28)
	case offset && longitudeByte >= 108 && longitudeByte <= 117:
		longitude = float64(longitudeByte - 8)
	case offset && longitudeByte >= 38 && longitudeByte <= 107:
		longitude = float64(longitudeByte + 72)
	default:
		return fmt.Errorf("invalid Mic-E longitude degrees")
	}
	minuteByte := int(info[2])
	switch {
	case minuteByte >= 88 && minuteByte <= 97:
		longitude += float64(minuteByte-88) / 60
	case minuteByte >= 38 && minuteByte <= 87:
		longitude += float64(minuteByte-28) / 60
	default:
		return fmt.Errorf("invalid Mic-E longitude minutes")
	}
	hundredthsByte := int(info[3])
	if hundredthsByte < 28 || hundredthsByte > 127 {
		return fmt.Errorf("invalid Mic-E longitude hundredths")
	}
	longitude += float64(hundredthsByte-28) / 6000
	if destination[5] >= 'P' && destination[5] <= 'Z' {
		longitude = -longitude
	}

	position.Latitude = latitude
	position.Longitude = longitude
	position.Symbol = string([]byte{info[8], info[7]})
	speed := (int(info[4])-28)*10 + (int(info[5])-28)/10
	if speed >= 800 {
		speed -= 800
	}
	if speed >= 0 {
		value := float64(speed)
		position.SpeedKnots = &value
	}
	course := ((int(info[5])-28)%10)*100 + int(info[6]) - 28
	if course >= 400 {
		course -= 400
	}
	if course > 0 && course <= 360 {
		if course == 360 {
			course = 0
		}
		value := float64(course)
		position.CourseDegrees = &value
	}

	comment := info[9:]
	if len(comment) > 0 && (comment[0] == ' ' || comment[0] == '>' || comment[0] == ']' || comment[0] == '`' || comment[0] == '\'') {
		comment = comment[1:]
	}
	if len(comment) >= 4 && base91Byte(comment[0]) && base91Byte(comment[1]) && base91Byte(comment[2]) && comment[3] == '}' {
		meters := float64(decodeBase91(comment[:3]) - 10000)
		feet := meters * 3.280839895
		position.AltitudeFeet = &feet
		comment = comment[4:]
	}
	extractAltitude(comment, position)
	position.Comment = strings.TrimSpace(altitudePattern.ReplaceAllString(comment, ""))
	return nil
}

func micEDigit(value byte) (int, bool) {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0'), true
	case value >= 'A' && value <= 'J':
		return int(value - 'A'), true
	case value >= 'P' && value <= 'Y':
		return int(value - 'P'), true
	case value == 'K' || value == 'L' || value == 'Z':
		return 0, true
	default:
		return 0, false
	}
}

func extractAltitude(comment string, position *APRSPosition) {
	match := altitudePattern.FindStringSubmatch(comment)
	if len(match) != 2 {
		return
	}
	if altitude, err := strconv.ParseFloat(match[1], 64); err == nil {
		position.AltitudeFeet = &altitude
	}
}

func base91Byte(value byte) bool {
	return value >= 33 && value <= 123
}

func decodeBase91(value string) int {
	result := 0
	for index := range value {
		result = result*91 + int(value[index]-33)
	}
	return result
}
