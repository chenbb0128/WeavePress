package editorial

import "fmt"

type ThemeTokens struct {
	Accent     string `json:"accent"`
	Text       string `json:"text"`
	Muted      string `json:"muted"`
	Surface    string `json:"surface"`
	Border     string `json:"border"`
	Heading    string `json:"heading"`
	FontFamily string `json:"fontFamily"`
}

type Theme struct {
	ThemeSummary
	BodyStyle      string
	ParagraphStyle string
	Heading2Style  string
	Heading3Style  string
	QuoteStyle     string
	ListStyle      string
	DividerStyle   string
	LinkStyle      string
	CaptionStyle   string
}

type ThemeSummary struct {
	ID      string      `json:"id"`
	Version uint        `json:"version"`
	Name    string      `json:"name"`
	Preview string      `json:"preview"`
	Tokens  ThemeTokens `json:"tokens"`
}

var themes = []Theme{
	newTheme("minimal-business", "极简商务", "#2f3542", "#262626", "#f7f8fa", "#d9d9d9"),
	newTheme("clear-blue", "清爽蓝", "#1677ff", "#1f2937", "#f0f7ff", "#91caff"),
	newTheme("natural-green", "自然绿", "#389e0d", "#243126", "#f3f8ee", "#95de64"),
	newTheme("warm-lifestyle", "暖生活", "#d46b08", "#3d2b1f", "#fff7e6", "#ffd591"),
	newTheme("elegant-chinese", "雅致中国风", "#8c2f39", "#2f2725", "#faf5ef", "#d6bfa8"),
	newTheme("vibrant-brand", "活力品牌", "#722ed1", "#24212b", "#f9f0ff", "#d3adf7"),
}

func newTheme(id, name, accent, text, surface, border string) Theme {
	return Theme{
		ThemeSummary: ThemeSummary{
			ID:      id,
			Version: 1,
			Name:    name,
			Preview: accent,
			Tokens: ThemeTokens{
				Accent: accent, Text: text, Muted: "#6b7280", Surface: surface, Border: border,
				Heading: text, FontFamily: "-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif",
			},
		},
		BodyStyle:      "color:" + text + ";font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;font-size:16px;line-height:1.85",
		ParagraphStyle: "margin:0 0 16px",
		Heading2Style:  "margin:32px 0 16px;padding-left:12px;border-left:4px solid " + accent + ";color:" + text + ";font-size:22px;line-height:1.4",
		Heading3Style:  "margin:24px 0 12px;color:" + text + ";font-size:18px;line-height:1.5",
		QuoteStyle:     "margin:20px 0;padding:12px 16px;border-left:4px solid " + accent + ";background:" + surface + ";color:" + text,
		ListStyle:      "margin:0 0 16px;padding-left:24px",
		DividerStyle:   "margin:28px 0;border:0;border-top:1px solid " + border,
		LinkStyle:      "color:" + accent + ";text-decoration:underline",
		CaptionStyle:   "margin-top:8px;color:#6b7280;font-size:13px;line-height:1.6;text-align:center",
	}
}

func ListThemes() []ThemeSummary {
	result := make([]ThemeSummary, len(themes))
	for index, theme := range themes {
		result[index] = theme.ThemeSummary
	}
	return result
}

func ResolveTheme(id string, version uint) (Theme, error) {
	for _, theme := range themes {
		if theme.ID == id && theme.Version == version {
			return theme, nil
		}
	}
	return Theme{}, fmt.Errorf("%w: %s@%d", ErrThemeNotFound, id, version)
}
