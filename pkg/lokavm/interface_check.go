package lokavm

// Compile-time interface checks.
var (
	_ Hypervisor = (*VZHypervisor)(nil)
	_ Hypervisor = (*KVMHypervisor)(nil)
	_ Hypervisor = (*CHHypervisor)(nil)
)
