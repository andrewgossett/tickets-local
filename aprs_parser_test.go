package main

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseAPRSPositionFormats(t *testing.T) {
	timestamp := time.Date(2026, 7, 24, 15, 30, 0, 0, time.UTC)
	compressed := compressedPacket("W1AW-9", 41.7148, -72.7272)
	tests := []struct {
		name       string
		packet     string
		latitude   float64
		longitude  float64
		speed      float64
		course     float64
		altitude   float64
		wantSpeed  bool
		wantCourse bool
	}{
		{
			name:       "uncompressed",
			packet:     "N0CALL-7>APRS,WIDE1-1:!3503.50N/08640.25W>123/045/A=001234 Mobile",
			latitude:   35.058333,
			longitude:  -86.670833,
			speed:      45,
			course:     123,
			altitude:   1234,
			wantSpeed:  true,
			wantCourse: true,
		},
		{
			name:      "compressed",
			packet:    compressed,
			latitude:  41.7148,
			longitude: -72.7272,
		},
		{
			name:       "Mic-E",
			packet:     "N1ZZN-9>T2SP0W:`c_Vm6hk/`\"49}Jeff Mobile_%",
			latitude:   42.501167,
			longitude:  -71.126333,
			speed:      12,
			course:     276,
			altitude:   111.5486,
			wantSpeed:  true,
			wantCourse: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			position, err := parseAPRSPacket(test.packet, timestamp)
			if err != nil {
				t.Fatalf("parseAPRSPacket: %v", err)
			}
			if math.Abs(position.Latitude-test.latitude) > 0.00002 ||
				math.Abs(position.Longitude-test.longitude) > 0.00002 {
				t.Fatalf("position = %.6f, %.6f; want %.6f, %.6f", position.Latitude, position.Longitude, test.latitude, test.longitude)
			}
			if position.ReceivedAt != timestamp || position.Callsign == "" {
				t.Fatalf("metadata was not retained: %+v", position)
			}
			if test.wantSpeed && (position.SpeedKnots == nil || *position.SpeedKnots != test.speed) {
				t.Fatalf("speed = %v, want %.1f", position.SpeedKnots, test.speed)
			}
			if test.wantCourse && (position.CourseDegrees == nil || *position.CourseDegrees != test.course) {
				t.Fatalf("course = %v, want %.1f", position.CourseDegrees, test.course)
			}
			if test.altitude != 0 && (position.AltitudeFeet == nil || math.Abs(*position.AltitudeFeet-test.altitude) > 0.01) {
				t.Fatalf("altitude = %v, want %.2f", position.AltitudeFeet, test.altitude)
			}
		})
	}
}

func TestParseAPRSPacketRejectsUnsupportedAndMalformedPackets(t *testing.T) {
	for _, packet := range []string{
		"# server comment",
		"N0CALL>APRS:>status only",
		"N0CALL>APRS:!9999.99N/99999.99W>",
		"not a packet",
	} {
		if _, err := parseAPRSPacket(packet, time.Now()); err == nil {
			t.Fatalf("parseAPRSPacket accepted %q", packet)
		}
	}
}

func TestParseAPRSPacketDecodesBoundedWeatherObservation(t *testing.T) {
	observedAt := time.Date(2026, 9, 6, 15, 0, 0, 0, time.UTC)
	position, err := parseAPRSPacket("WX1>APRS:!3503.50N/08640.25W_ c180 s012 g025 t084 r005 p123 P045 h65 b10132", observedAt)
	if err != nil {
		t.Fatal(err)
	}
	weather := position.Weather
	if weather == nil || weather.TemperatureF == nil || *weather.TemperatureF != 84 ||
		weather.WindDirection == nil || *weather.WindDirection != 180 ||
		weather.WindSpeedMPH == nil || *weather.WindSpeedMPH != 12 ||
		weather.WindGustMPH == nil || *weather.WindGustMPH != 25 ||
		weather.RainLastHourInches == nil || *weather.RainLastHourInches != .05 ||
		weather.Rain24HoursInches == nil || *weather.Rain24HoursInches != 1.23 ||
		weather.HumidityPercent == nil || *weather.HumidityPercent != 65 ||
		weather.PressureMillibars == nil || *weather.PressureMillibars != 1013.2 ||
		!weather.ObservedAt.Equal(observedAt) {
		t.Fatalf("unexpected APRS weather observation: %+v", weather)
	}
}

func TestParseAPRSWeatherIgnoresLookalikesAndBoundsValues(t *testing.T) {
	if got := parseAPRSWeather("status c180 s012", ">/", time.Now()); got != nil {
		t.Fatalf("ordinary short comment decoded as weather: %+v", got)
	}
	if got := parseAPRSWeather("c999 s999 t999 h999 b99999", "/_", time.Now()); got != nil {
		t.Fatalf("out-of-range weather fields were accepted: %+v", got)
	}
}

func compressedPacket(callsign string, latitude, longitude float64) string {
	latValue := int(math.Round((90 - latitude) * 380926))
	lonValue := int(math.Round((longitude + 180) * 190463))
	return fmt.Sprintf("%s>APRS:!/%s%s>", callsign, encodeBase91(latValue, 4), encodeBase91(lonValue, 4))
}

func encodeBase91(value, length int) string {
	result := make([]byte, length)
	for index := length - 1; index >= 0; index-- {
		result[index] = byte(value%91 + 33)
		value /= 91
	}
	return strings.TrimSpace(string(result))
}
