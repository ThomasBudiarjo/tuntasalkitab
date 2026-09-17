package reading

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
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

	sum := sha256.Sum256(data)
	planVersion = hex.EncodeToString(sum[:8])
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
	Month     int
	MonthName string
	Year      int
	Days      []DayInfo
	TotalDays int
	StartDay  int
}

type DayInfo struct {
	Day          int
	DayOfYear    int
	Passage      string
	PassageLinks []PassageLink
	Completed    bool
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
		passage := GetPassageByDayOfYear(dayOfYear)
		days[d-1] = DayInfo{
			Day:          d,
			DayOfYear:    dayOfYear,
			Passage:      passage,
			PassageLinks: ParsePassages(passage),
			Completed:    completedDays[dayOfYear],
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

// planVersion is the content hash of plan.json, computed once at startup.
var planVersion string

// PlanVersion identifies the current reading plan.
//
// It is derived from the file's contents rather than a hand-maintained
// constant: the plan has already been corrected twice, and a version someone
// has to remember to bump is a version that eventually does not get bumped —
// leaving installed apps silently on a stale plan.
//
// The mobile client stores this alongside its copy and replaces the plan when
// the server reports a different value.
func PlanVersion() string {
	return planVersion
}

// Days returns the whole plan, keyed by day of year as a string.
func Days() map[string]string {
	days := make(map[string]string, len(readingPlan))
	for k, v := range readingPlan {
		days[k] = v
	}
	return days
}
