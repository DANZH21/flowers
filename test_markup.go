package main

import (
	"fmt"

	tele "gopkg.in/telebot.v3"
)

func main() {
	menu := &tele.ReplyMarkup{}
	var btnRows []tele.Row
	var allTimeBtns []tele.Btn

	for hour := 10; hour < 20; hour++ {
		for min := 0; min < 60; min += 30 {
			timeStr := fmt.Sprintf("%02d:%02d", hour, min)
			btn := menu.Data(
				timeStr,
				fmt.Sprintf("select_time_%s", timeStr),
			)
			allTimeBtns = append(allTimeBtns, btn)
		}
	}

	for i := 0; i < len(allTimeBtns); i += 3 {
		end := i + 3
		if end > len(allTimeBtns) {
			end = len(allTimeBtns)
		}
		btnRows = append(btnRows, menu.Row(allTimeBtns[i:end]...))
		fmt.Printf("Row: %v\n", allTimeBtns[i:end])
	}

	fmt.Printf("Total rows: %d\n", len(btnRows))
}
