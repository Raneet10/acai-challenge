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
	"strings"
)

const wikipediaAPIBaseURL = "https://en.wikipedia.org/w/api.php"

type wikiSearchResponse struct {
	Query struct {
		Search []struct {
			Title string `json:"title"`
		} `json:"search"`
	} `json:"query"`
}

// searchAttractions returns notable points of interest for a destination using
// Wikipedia's search API, which is free and requires no API key.
func searchAttractions(ctx context.Context, destination string) ([]string, error) {
	query := url.Values{
		"action":   {"query"},
		"list":     {"search"},
		"srsearch": {"tourist attractions in " + destination},
		"srlimit":  {"8"},
		"format":   {"json"},
	}

	endpoint := wikipediaAPIBaseURL + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build Wikipedia API request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Wikipedia API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Wikipedia API response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikipedia API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data wikiSearchResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to parse Wikipedia API response: %w", err)
	}

	titles := make([]string, len(data.Query.Search))
	for i, s := range data.Query.Search {
		titles[i] = s.Title
	}

	return titles, nil
}

// PlanItinerary returns real-world trip-planning context for a destination: a
// day-by-day weather forecast plus notable points of interest.
func PlanItinerary(ctx context.Context, destination string, days int) (string, error) {
	slog.InfoContext(ctx, "Planning itinerary", "destination", destination, "days", days)

	forecast, err := GetWeatherForecast(ctx, destination, days)
	if err != nil {
		return "", err
	}

	attractions, err := searchAttractions(ctx, destination)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(forecast)
	b.WriteString("\n\nPossible points of interest in ")
	b.WriteString(destination)
	b.WriteString(": ")
	b.WriteString(strings.Join(attractions, ", "))

	return b.String(), nil
}

func handlePlanItinerary(ctx context.Context, rawArgs string) (string, error) {
	var args struct {
		Destination string `json:"destination"`
		Days        int    `json:"days,omitempty"`
	}

	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return "", errors.New("failed to parse tool call arguments: " + err.Error())
	}

	itinerary, err := PlanItinerary(ctx, args.Destination, args.Days)
	if err != nil {
		return "", errors.New("failed to plan itinerary: " + err.Error())
	}

	return itinerary, nil
}
