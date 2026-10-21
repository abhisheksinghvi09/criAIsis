package oncall

import "net/http"

// ForTest builds listers against a fake server base URL.
func PagerDutyForTest(base string, c *http.Client) Lister {
	return &pagerDuty{c: c, base: base, key: "k", schedule: "S1"}
}

func OpsgenieForTest(base string, c *http.Client) Lister {
	return &opsgenie{c: c, base: base, key: "k", schedule: "S1"}
}
