package drivers

import (
	"regexp"
	"slices"
	"strings"
)

const (
	ObjectTypeProcedure = "procedure"
	ObjectTypeFunction  = "function"
	ObjectTypeTrigger   = "trigger"
	ObjectTypeEvent     = "event"
	ObjectTypeSequence  = "sequence"
	ObjectTypeSynonym   = "synonym"
	ObjectTypePackage   = "package"
	ObjectTypeMatView   = "matview"
	ObjectTypeType      = "type"
)

var AllObjectTypes = []string{
	ObjectTypeProcedure, ObjectTypeFunction, ObjectTypeTrigger, ObjectTypeEvent,
	ObjectTypeSequence, ObjectTypeSynonym, ObjectTypePackage,
	ObjectTypeMatView, ObjectTypeType,
}

var DroppableObjectTypes = []string{
	ObjectTypeTrigger, ObjectTypeEvent, ObjectTypeSequence,
	ObjectTypeSynonym, ObjectTypeMatView, ObjectTypeProcedure, ObjectTypeFunction,
}

func IsValidObjectType(t string) bool {
	return slices.Contains(AllObjectTypes, t)
}

var delimiterCmdRe = regexp.MustCompile(`(?im)^\s*DELIMITER\s+(\S+)\s*$`)

func StripDelimiter(sql string) string {
	lines := strings.Split(sql, "\n")
	currentDelim := ""
	var result []string
	for _, line := range lines {
		if m := delimiterCmdRe.FindStringSubmatch(line); m != nil {
			newDelim := m[1]
			if newDelim == ";" {
				currentDelim = ""
			} else {
				currentDelim = newDelim
			}
			continue
		}
		if currentDelim != "" {
			line = strings.ReplaceAll(line, currentDelim, ";")
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}
