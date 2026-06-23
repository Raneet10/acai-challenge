package tools

import "github.com/openai/openai-go/v2"

// Default returns the full set of tools available to the assistant.
//
// This is the single place tools are registered. To add a new tool: write its
// handler (and any supporting logic) in its own file — see weather.go,
// calendar.go, datetime.go, or itinerary.go for examples — then append one
// entry here.
func Default() Registry {
	return Registry{
		{
			Name:        "get_weather",
			Description: "Get the current weather conditions at the given location",
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"location": weatherLocationParam,
				},
				"required": []string{"location"},
			},
			Handler: handleGetWeather,
		},
		{
			Name:        "get_weather_forecast",
			Description: "Get the multi-day weather forecast for the given location",
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"location": weatherLocationParam,
					"days": map[string]string{
						"type":        "integer",
						"description": "Number of forecast days to return, from 1 to 10. Defaults to 3 if not provided.",
					},
				},
				"required": []string{"location"},
			},
			Handler: handleGetWeatherForecast,
		},
		{
			Name:        "get_today_date",
			Description: "Get today's date and time in RFC3339 format",
			Handler:     handleGetTodayDate,
		},
		{
			Name:        "get_holidays",
			Description: "Gets local bank and public holidays. Each line is a single holiday in the format 'YYYY-MM-DD: Holiday Name'.",
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"before_date": map[string]string{
						"type":        "string",
						"description": "Optional date in RFC3339 format to get holidays before this date. If not provided, all holidays will be returned.",
					},
					"after_date": map[string]string{
						"type":        "string",
						"description": "Optional date in RFC3339 format to get holidays after this date. If not provided, all holidays will be returned.",
					},
					"max_count": map[string]string{
						"type":        "integer",
						"description": "Optional maximum number of holidays to return. If not provided, all holidays will be returned.",
					},
				},
			},
			Handler: handleGetHolidays,
			Init:    loadHolidayCalendar,
		},
		{
			Name:        "plan_itinerary",
			Description: "Get real-world trip-planning context for a destination: a day-by-day weather forecast plus notable points of interest, to help draft a travel itinerary",
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"destination": weatherLocationParam,
					"days": map[string]string{
						"type":        "integer",
						"description": "Number of days of the trip, from 1 to 10. Defaults to 3 if not provided.",
					},
				},
				"required": []string{"destination"},
			},
			Handler: handlePlanItinerary,
		},
	}
}
