package logging

// RegisterFieldAliases extends the field names the parser recognizes for level,
// message, and timestamp, so a team's bespoke schema (e.g. a custom "lvl" or
// "@l" level key) is understood without code changes. The aliases feed both the
// JSON and logfmt parsers, so a key configured once is honored regardless of the
// line's format. It is additive (built-in names keep priority) and meant to be
// called once at startup, before any parsing. The formatted-field exclusion set
// is rebuilt so newly recognized level/timestamp keys are not also printed as
// ordinary fields.
func RegisterFieldAliases(levelFields, messageFields, timeFields []string) {
	jsonLevelFields = appendUnique(jsonLevelFields, levelFields)
	jsonMessageFields = appendUnique(jsonMessageFields, messageFields)
	jsonTimeFields = appendUnique(jsonTimeFields, timeFields)

	logfmtLevelFields = appendUnique(logfmtLevelFields, levelFields)
	logfmtMessageFields = appendUnique(logfmtMessageFields, messageFields)
	logfmtTimeFields = appendUnique(logfmtTimeFields, timeFields)

	jsonFormattedFieldExclusions = buildStringSet(jsonLevelFields, jsonTimeFields)
	jsonMessageFieldSet = buildStringSet(jsonMessageFields)
}

// appendUnique appends each value not already present in base, preserving order.
func appendUnique(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base))
	for _, v := range base {
		seen[v] = struct{}{}
	}
	for _, v := range extra {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		base = append(base, v)
	}
	return base
}
