package lifecycle

// Confirmations is how many confirmations the journey's operator was asked.
func (j *CapabilityJourney) Confirmations() int { return j.harness.confirmer.asked }
