package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const weatherAPIBaseURL = "https://api.weatherapi.com/v1"

// weatherLocationParam is the shared "location" parameter schema for the weather tools.
var weatherLocationParam = map[string]string{
	"type":        "string",
	"description": "City name, optionally with country, e.g. 'Barcelona' or 'Barcelona, Spain'",
}

type weatherCondition struct {
	Text string `json:"text"`
}

type weatherLocation struct {
	Name    string `json:"name"`
	Country string `json:"country"`
}

type currentWeather struct {
	TempC      float64          `json:"temp_c"`
	FeelsLikeC float64          `json:"feelslike_c"`
	WindKph    float64          `json:"wind_kph"`
	Humidity   int              `json:"humidity"`
	Condition  weatherCondition `json:"condition"`
}

type currentWeatherResponse struct {
	Location weatherLocation `json:"location"`
	Current  currentWeather  `json:"current"`
}

type forecastDay struct {
	Date string `json:"date"`
	Day  struct {
		MaxTempC          float64          `json:"maxtemp_c"`
		MinTempC          float64          `json:"mintemp_c"`
		AvgTempC          float64          `json:"avgtemp_c"`
		DailyChanceOfRain int              `json:"daily_chance_of_rain"`
		Condition         weatherCondition `json:"condition"`
	} `json:"day"`
}

type forecastResponse struct {
	Location weatherLocation `json:"location"`
	Forecast struct {
		ForecastDay []forecastDay `json:"forecastday"`
	} `json:"forecast"`
}

func weatherAPIGet(ctx context.Context, path string, query url.Values) ([]byte, error) {
	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("WEATHER_API_KEY is not set")
	}

	query.Set("key", apiKey)
	endpoint := weatherAPIBaseURL + path + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build weather API request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call weather API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read weather API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("weather API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return body, nil
}

// GetCurrentWeather returns a human-readable summary of current conditions at the given location.
func GetCurrentWeather(ctx context.Context, location string) (string, error) {
	slog.InfoContext(ctx, "Fetching current weather", "location", location)

	body, err := weatherAPIGet(ctx, "/current.json", url.Values{"q": {location}})
	if err != nil {
		return "", err
	}

	var data currentWeatherResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("failed to parse weather API response: %w", err)
	}

	return fmt.Sprintf(
		"Current weather in %s, %s: %s, %.1f°C (feels like %.1f°C), wind %.1f kph, humidity %d%%.",
		data.Location.Name, data.Location.Country,
		data.Current.Condition.Text, data.Current.TempC, data.Current.FeelsLikeC,
		data.Current.WindKph, data.Current.Humidity,
	), nil
}

// GetWeatherForecast returns a human-readable, day-by-day forecast for the given location.
// days is clamped to the 1-10 range supported by WeatherAPI's free tier, defaulting to 3.
func GetWeatherForecast(ctx context.Context, location string, days int) (string, error) {
	switch {
	case days <= 0:
		days = 3
	case days > 10:
		days = 10
	}

	slog.InfoContext(ctx, "Fetching weather forecast", "location", location, "days", days)

	body, err := weatherAPIGet(ctx, "/forecast.json", url.Values{
		"q":    {location},
		"days": {fmt.Sprintf("%d", days)},
	})
	if err != nil {
		return "", err
	}

	var data forecastResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("failed to parse weather API response: %w", err)
	}

	lines := []string{fmt.Sprintf("Forecast for %s, %s:", data.Location.Name, data.Location.Country)}
	for _, d := range data.Forecast.ForecastDay {
		lines = append(lines, fmt.Sprintf(
			"%s: %s, %.1f°C to %.1f°C (avg %.1f°C), %d%% chance of rain",
			d.Date, d.Day.Condition.Text, d.Day.MinTempC, d.Day.MaxTempC, d.Day.AvgTempC, d.Day.DailyChanceOfRain,
		))
	}

	return strings.Join(lines, "\n"), nil
}

func handleGetWeather(ctx context.Context, rawArgs string) (string, error) {
	var args struct {
		Location string `json:"location"`
	}

	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", errors.New("failed to parse tool call arguments: " + err.Error())
	}

	weather, err := GetCurrentWeather(ctx, args.Location)
	if err != nil {
		return "", errors.New("failed to get weather: " + err.Error())
	}

	return weather, nil
}

func handleGetWeatherForecast(ctx context.Context, rawArgs string) (string, error) {
	var args struct {
		Location string `json:"location"`
		Days     int    `json:"days,omitempty"`
	}

	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", errors.New("failed to parse tool call arguments: " + err.Error())
	}

	forecast, err := GetWeatherForecast(ctx, args.Location, args.Days)
	if err != nil {
		return "", errors.New("failed to get weather forecast: " + err.Error())
	}

	return forecast, nil
}
