package value

import (
	"fmt"
	"strings"
)

// PersonaKey represents one of the four fixed infrastructure domain specialists.
// We strictly enforce this domain set at the boundary to eliminate arbitrary persona drift.
type PersonaKey string

const (
	PersonaKeyNetwork     PersonaKey = "network"
	PersonaKeyDatabase    PersonaKey = "database"
	PersonaKeyApplication PersonaKey = "application"
	PersonaKeySecurity    PersonaKey = "security"
)

// ParsePersonaKey canonicalizes and validates untrusted persona input from Slack commands or API payloads.
func ParsePersonaKey(raw string) (PersonaKey, error) {
	canonical := strings.ToLower(strings.TrimSpace(raw))
	switch canonical {
	case "network":
		return PersonaKeyNetwork, nil
	case "database":
		return PersonaKeyDatabase, nil
	case "application":
		return PersonaKeyApplication, nil
	case "security":
		return PersonaKeySecurity, nil
	default:
		return "", fmt.Errorf("invalid persona key '%s': must be network, database, application, or security", raw)
	}
}

// String returns the canonical key identifier.
func (p PersonaKey) String() string {
	return string(p)
}

// AllPersonaKeys returns the immutable set of four domain specialists in deterministic order.
func AllPersonaKeys() []PersonaKey {
	return []PersonaKey{
		PersonaKeyNetwork,
		PersonaKeyDatabase,
		PersonaKeyApplication,
		PersonaKeySecurity,
	}
}
