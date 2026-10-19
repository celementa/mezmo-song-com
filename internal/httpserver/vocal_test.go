package httpserver

import "testing"

func TestVocalGenderFromText(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		// Реальный кейс жалобы: просьба в поле «Опишите правки».
		{"Сделайте мужской голос", "male"},
		{"хочу мужской вокал и побыстрее темп", "male"},
		{"пусть поёт мужчина", "male"},
		{"спойте женским голосом", "female"},
		{"пусть пела женщина", "female"},
		{"male voice please", "male"},
		{"female vocals", "female"},
		// Последнее упоминание побеждает.
		{"замените женский голос на мужской", "male"},
		{"замените мужской голос на женский", "female"},
		// Пол без вокального контекста — не просьба сменить голос.
		{"песня для мужа, добавьте про рыбалку", ""},
		{"поменяйте вторую строчку", ""},
		{"сделайте голос повыше", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := vocalGenderFromText(c.text); got != c.want {
			t.Errorf("vocalGenderFromText(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}
