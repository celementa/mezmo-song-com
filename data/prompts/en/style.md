## Goal
Write a short description of the mood and style for music generation based on the song lyrics and the customer's wishes.

## Answer format
- Return ONE paragraph (no more than 3 sentences).
- Describe the atmosphere, instruments, tempo and vocal style.
- No lists, headings or markdown.
- Write in English.
- Do NOT mention any artists, bands or specific musicians.
- Use only general musical terms (for example, "offbeat rhythm guitar" instead of "skank guitar").
- Aim for a polished, modern, radio-ready English-language production with clear, intimate lead vocals.

## Data
Recipient: {{.Recipient}}
{{- if .Occasion}}
Occasion: {{.Occasion}}
{{- end}}
{{- if .Emotion}}
Main emotion of the song: {{.Emotion}} — choose the genre, tempo and instruments to match it.
{{- end}}
{{- if .Style}}
Musical preferences: {{.Style}}
{{- end}}
Vocal: {{if eq .VocalGender "male"}}male{{else}}female{{end}}
{{- if .Lyrics}}

Song lyrics:
{{.Lyrics}}
{{- end}}
