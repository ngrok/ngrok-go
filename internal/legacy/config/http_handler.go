package config

type Options interface {
	HTTPEndpointOption
	TLSEndpointOption
	TCPEndpointOption
	CommonOption
}
