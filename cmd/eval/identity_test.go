package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

func TestRoleCountCriterionAcceptsFormattingButRejectsWrongCounts(t *testing.T) {
	raw, err := os.ReadFile("../../eval/probes/45-assistant-role.json")
	if err != nil {
		t.Fatal(err)
	}
	var probe Probe
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	count, err := regexp.Compile(probe.Answer[1].Regex)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		"В этом разговоре был **1 реальный пользовательский запрос**.",
		"**Реальных пользовательских запросов в разговоре:** 1.",
		"Пользовательских запросов: один.",
		"There was 1 user request.",
	} {
		if !count.MatchString(text) {
			t.Fatal("rejected correct count", text)
		}
	}
	for _, text := range []string{
		"Было 2 пользовательских запроса.",
		"**2 запроса.**",
		"11 пользовательских запросов",
		"Реальных пользовательских запросов: 21.",
		"There were 2 user requests.",
	} {
		if count.MatchString(text) {
			t.Fatal("accepted wrong count", text)
		}
	}
}
