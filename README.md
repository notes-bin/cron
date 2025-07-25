# cron

A simple cron-like job scheduler for self-use.

## Installation
```bash
go get github.com/notes-bin/cron
```

## Usage Example

```go
package main

import (
	"fmt"
	"time"
	"github.com/notes-bin/cron"
)

// ExampleJob is a simple job that prints a message
type ExampleJob struct {
	Message string
}

func (e *ExampleJob) Run() {
	fmt.Printf("%s at %v\n", e.Message, time.Now())
}

func main() {
	// Create a new cron scheduler
	c := cron.New()

	// Add a job to run every hour
	hourlyJob := &ExampleJob{Message: "This runs every hour"}
	c.AddJob(&HourlySchedule{}, hourlyJob)

	// Add a function to run every minute
	c.AddFunc(&MinuteSchedule{}, func() {
		fmt.Printf("This runs every minute at %v\n", time.Now())
	})

	// Start the scheduler
	c.Start()
	defer c.Stop()

	// Keep the program running
	select {}
}

// HourlySchedule implements cron.Schedule
type HourlySchedule struct {}

func (s *HourlySchedule) Next(t time.Time) time.Time {
	return t.Add(1 * time.Hour)
}

// MinuteSchedule implements cron.Schedule
type MinuteSchedule struct {}

func (s *MinuteSchedule) Next(t time.Time) time.Time {
	return t.Add(1 * time.Minute)
}
```

## Testing
Run the tests with:
```bash
go test -v
```

## Schedule Implementations
The cron package doesn't provide schedule implementations by default, but you can implement the `Schedule` interface:

```go
// DailySchedule runs once per day at the same time
type DailySchedule struct {
	Hour, Minute int
}

func (s *DailySchedule) Next(t time.Time) time.Time {
	next := time.Date(t.Year(), t.Month(), t.Day(), s.Hour, s.Minute, 0, 0, t.Location())
	if next.Before(t) {
		next = next.Add(24 * time.Hour)
	}
	return next
}
```

## License
MIT
