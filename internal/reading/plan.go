package reading

import (
	"embed"
	"encoding/json"
	"time"
)

//go:embed plan.json
var planData embed.FS

type Plan map[string]string

var readingPlan Plan

func init() {
	data, err := planData.ReadFile("plan.json")
	if err != nil {
		panic("failed to load reading plan: " + err.Error())
	}
	if err := json.Unmarshal(data, &readingPlan); err != nil {
		panic("failed to parse reading plan: " + err.Error())
	}
}

func GetPassage(dayOfYear int) string {
	if dayOfYear < 1 || dayOfYear > 365 {
		return ""
	}
	return readingPlan[string(rune('0'+dayOfYear/100))+string(rune('0'+(dayOfYear%100)/10))+string(rune('0'+dayOfYear%10))]
}

func GetPassageByKey(day string) string {
	return readingPlan[day]
}

type MonthInfo struct {
	Month      int
	MonthName  string
	Year       int
	Days       []DayInfo
	TotalDays  int
	StartDay   int
}

type DayInfo struct {
	Day       int
	DayOfYear int
	Passage   string
	Completed bool
}

var monthNames = []string{
	"", "January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

func GetMonthInfo(year, month int, completedDays map[int]bool) MonthInfo {
	firstDay := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	lastDay := firstDay.AddDate(0, 1, -1)
	numDays := lastDay.Day()

	days := make([]DayInfo, numDays)
	for d := 1; d <= numDays; d++ {
		date := time.Date(year, time.Month(month), d, 0, 0, 0, 0, time.UTC)
		dayOfYear := date.YearDay()
		days[d-1] = DayInfo{
			Day:       d,
			DayOfYear: dayOfYear,
			Passage:   GetPassageByDayOfYear(dayOfYear),
			Completed: completedDays[dayOfYear],
		}
	}

	startDayOfYear := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).YearDay()

	return MonthInfo{
		Month:     month,
		MonthName: monthNames[month],
		Year:      year,
		Days:      days,
		TotalDays: numDays,
		StartDay:  startDayOfYear,
	}
}

func GetPassageByDayOfYear(dayOfYear int) string {
	if dayOfYear < 1 || dayOfYear > 365 {
		return ""
	}
	key := itoa(dayOfYear)
	return readingPlan[key]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func GetCurrentMonth() int {
	return int(time.Now().Month())
}

func GetCurrentYear() int {
	return time.Now().Year()
}

