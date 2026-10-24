package nativesave

func sessionAllocation(s SessionState) string {
	if s.Allocation != "" {
		return s.Allocation
	}
	return s.Campaign
}
