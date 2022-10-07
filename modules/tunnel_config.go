package modules

// TunnelOptions is a marker trait for options that can be used to start
// tunnels.
// It should not be implemented outside of this module.
type TunnelOptions interface {
	tunnelOptions()
}
