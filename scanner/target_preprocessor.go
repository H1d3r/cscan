package scanner

import (
	"regexp"
	"strings"
)

var (
	zeroWidthRegex      = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}\x{200E}\x{200F}\x{202A}-\x{202E}\x{2060}]`)
	rangeSpaceRegex     = regexp.MustCompile(`\s+-\s+`)
	delimiterRegex      = regexp.MustCompile(`[;,\t]+`)
	multipleSpacesRegex = regexp.MustCompile(`[ ]+`)
	multiPortRegex      = regexp.MustCompile(`^([^:/]+):(\d+(?:,\d+)+)$`)
)

// NormalizeTargets cleans up Zero Width characters, Defanged URLs, and formatting.
// Note: It returns a slice of target strings, because it also splits comma-separated ports if necessary.
func (p *TargetParser) NormalizeTargets(input string) string {
	// 1. Remove Zero-width characters and BOM
	out := zeroWidthRegex.ReplaceAllString(input, "")

	// 2. Refang items
	replacements := []string{
		"[.]", ".",
		"(.)", ".",
		"[dot]", ".",
		"(dot)", ".",
		"hxxp://", "http://",
		"hXXp://", "http://",
		"h__p://", "http://",
		"hxxps://", "https://",
		"hXXps://", "https://",
		"h__ps://", "https://",
		"[:]", ":",
		"(:)", ":",
		"\r", "", // normalize return carriage
	}
	replacer := strings.NewReplacer(replacements...)
	out = replacer.Replace(out)

	// 3. Keep IP Range untouched around dashes
	out = rangeSpaceRegex.ReplaceAllString(out, "-")

	return out
}

func (p *TargetParser) splitAndSpread(input string) []string {
	var results []string

	// First convert all delimiters except comma to space
	// Wait, we need to handle "host:80,443" vs "host1,host2"
	// Let's replace ';' and '\t' with newline first
	out := strings.ReplaceAll(input, ";", "\n")
	out = strings.ReplaceAll(out, "\t", "\n")

	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Let's process commas
		// If line contains a comma, is it a list of targets? Or a port list?
		// We split by space first to treat each "word"
		words := multipleSpacesRegex.Split(line, -1)
		for _, word := range words {
			// If word looks like host:port,port,port
			if matches := multiPortRegex.FindStringSubmatch(word); len(matches) == 3 {
				host := matches[1]
				ports := strings.Split(matches[2], ",")
				for _, port := range ports {
					results = append(results, host+":"+port)
				}
			} else {
				// Otherwise, just split by comma
				parts := strings.Split(word, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if part != "" {
						results = append(results, part)
					}
				}
			}
		}
	}

	return results
}
