# Tickets Local sensor format v1

Local sensor endpoints return UTF-8 JSON with `Content-Type: application/json`.
Tickets Local polls only URLs explicitly entered by the operator. A response is
limited to 5 MB and 100 observations; at most 10 endpoints may be configured.

```json
{
  "version": 1,
  "observations": [
    {
      "id": "shelter-north",
      "name": "North Shelter",
      "observed_at": "2026-09-08T14:05:00Z",
      "latitude": 35.925,
      "longitude": -86.868,
      "temperature_f": 78.4,
      "humidity_percent": 61,
      "pressure_in_hg": 29.94,
      "wind_speed_mph": 8.2,
      "water_depth_feet": 0.3,
      "battery_percent": 87,
      "status": "normal"
    }
  ]
}
```

`id` and `observed_at` are required. Coordinates and measurements are optional.
Unknown JSON fields are ignored for forward compatibility. Invalid coordinates,
out-of-range measurements, missing identities, and unsupported versions are
discarded. Sensor observations are transient cached awareness data and never
enter `events.ndjson`.
