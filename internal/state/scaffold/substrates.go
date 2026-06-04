package scaffold

var Substrates = map[Provider]Substrate{
	ProviderEmulatedBareMetal: emulatedBareMetalSubstrate,
	ProviderBareMetal:         bareMetalSubstrate,
	ProviderVSphere:           vSphereSubstrate,
	ProviderKubeVirt:          kubeVirtSubstrate,
}
