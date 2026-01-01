package reading

import (
	"net/url"
	"strings"
)

// PassageLink represents a single Bible passage with its URL
type PassageLink struct {
	Text string
	URL  string
}

// Indonesian abbreviation to full book name mapping
var bookAbbreviations = map[string]string{
	// Old Testament
	"Kej.":  "Kejadian",
	"Kel.":  "Keluaran",
	"Im.":   "Imamat",
	"Bil.":  "Bilangan",
	"Ul.":   "Ulangan",
	"Yos.":  "Yosua",
	"Hak.":  "Hakim-hakim",
	"Rut":   "Rut",
	"1Sam.": "1 Samuel",
	"2Sam.": "2 Samuel",
	"1Raj.": "1 Raja-raja",
	"2Raj.": "2 Raja-raja",
	"1Taw.": "1 Tawarikh",
	"2Taw.": "2 Tawarikh",
	"Ezr.":  "Ezra",
	"Neh.":  "Nehemia",
	"Est.":  "Ester",
	"Ayb.":  "Ayub",
	"Mzm.":  "Mazmur",
	"Ams.":  "Amsal",
	"Pkh.":  "Pengkhotbah",
	"Kid.":  "Kidung Agung",
	"Yes.":  "Yesaya",
	"Yer.":  "Yeremia",
	"Rat.":  "Ratapan",
	"Yeh.":  "Yehezkiel",
	"Dan.":  "Daniel",
	"Hos.":  "Hosea",
	"Yl.":   "Yoel",
	"Am.":   "Amos",
	"Ob.":   "Obaja",
	"Yun.":  "Yunus",
	"Mi.":   "Mikha",
	"Nah.":  "Nahum",
	"Hab.":  "Habakuk",
	"Zef.":  "Zefanya",
	"Hag.":  "Hagai",
	"Za.":   "Zakharia",
	"Mal.":  "Maleakhi",

	// New Testament
	"Mat.":  "Matius",
	"Mrk.":  "Markus",
	"Luk.":  "Lukas",
	"Yoh.":  "Yohanes",
	"Kis.":  "Kisah Para Rasul",
	"Rm.":   "Roma",
	"1Kor.": "1 Korintus",
	"2Kor.": "2 Korintus",
	"Gal.":  "Galatia",
	"Ef.":   "Efesus",
	"Flp.":  "Filipi",
	"Kol.":  "Kolose",
	"1Tes.": "1 Tesalonika",
	"2Tes.": "2 Tesalonika",
	"1Tim.": "1 Timotius",
	"2Tim.": "2 Timotius",
	"Tit.":  "Titus",
	"Flm.":  "Filemon",
	"Ibr.":  "Ibrani",
	"Yak.":  "Yakobus",
	"1Ptr.": "1 Petrus",
	"2Ptr.": "2 Petrus",
	"1Yoh.": "1 Yohanes",
	"2Yoh.": "2 Yohanes",
	"3Yoh.": "3 Yohanes",
	"Yud.":  "Yudas",
	"Why.":  "Wahyu",
}

// ParsePassages splits a passage string and generates URLs for each
func ParsePassages(passageStr string) []PassageLink {
	if passageStr == "" {
		return nil
	}

	parts := strings.Split(passageStr, "; ")
	links := make([]PassageLink, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fullName := expandAbbreviation(part)
		links = append(links, PassageLink{
			Text: part,
			URL:  generateAlkitabURL(fullName),
		})
	}

	return links
}

// expandAbbreviation converts abbreviated book name to full name
func expandAbbreviation(passage string) string {
	for abbr, full := range bookAbbreviations {
		if strings.HasPrefix(passage, abbr) {
			return full + passage[len(abbr):]
		}
	}
	// Return as-is if no abbreviation found
	return passage
}

// generateAlkitabURL creates the alkitab.sabda.org URL
func generateAlkitabURL(passage string) string {
	// URL encode with + for spaces
	encoded := url.QueryEscape(passage)
	// url.QueryEscape uses %20, but the site prefers +
	encoded = strings.ReplaceAll(encoded, "%20", "+")
	return "https://alkitab.sabda.org/passage.php?passage=" + encoded
}
