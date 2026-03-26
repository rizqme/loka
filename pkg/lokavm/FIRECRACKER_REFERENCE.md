# Go KVM VMM — Firecracker Reference Guide

Reference from Firecracker's ARM64 implementation for building a pure Go KVM VMM.
Source: `/tmp/firecracker/src/vmm/src/arch/aarch64/`

## Architecture Overview

Firecracker's ARM64 boot sequence:

```
1. Create KVM VM          (KVM_CREATE_VM)
2. Setup memory regions   (KVM_SET_USER_MEMORY_REGION)
3. Create vCPUs           (KVM_CREATE_VCPU)  ← MUST be before GIC
4. Setup GIC (vGICv3)     (KVM_CREATE_DEVICE + attrs)
5. Load kernel to memory  (PE Image at DRAM_START + 2MB)
6. Load initramfs         (at end of DRAM - FDT_SIZE - initrd_size)
7. Generate FDT           (at end of DRAM - FDT_SIZE)
8. Set vCPU registers     (PC=kernel_entry, X0=fdt_addr, PSTATE)
9. Run vCPU loop          (KVM_RUN, handle MMIO exits)
```

## Memory Layout (ARM64)

```
Address          What
─────────────────────────────────────────
0x0000_0000      Reserved
0x4000_0000      MMIO 32-bit region start (1 GiB)
                 ├── Boot device (MMIO_LEN each)
                 ├── RTC (PL031)
                 ├── Serial (UART)
                 └── Virtio MMIO devices
~0x3FFF_0000     GIC redistributors (vcpu_count * 128K)
~0x3FFE_0000     GIC distributor (64K)
0x8000_0000      DRAM start (2 GiB)
0x8020_0000      System memory (2 MiB for ACPI)
0x8020_0000      Kernel load address (2MB aligned)
                 ...
DRAM_END-2MB     FDT (Flattened Device Tree, max 2MB)
DRAM_END-2MB-X   Initramfs
```

Key constants (from `layout.rs`):
```go
const DRAM_MEM_START    = 0x8000_0000  // 2 GiB
const SYSTEM_MEM_SIZE   = 0x0020_0000  // 2 MiB
const FDT_MAX_SIZE      = 0x0020_0000  // 2 MiB
const MMIO32_MEM_START  = 0x4000_0000  // 1 GiB
const MMIO_LEN          = 0x1000       // 4 KiB per device
```

## Step 1: GIC Setup (vGICv3)

Source: `gic/gicv3/mod.rs`

Order matters: **create vCPUs BEFORE GIC** (KVM requirement on ARM64).

```
1. KVM_CREATE_DEVICE(type=KVM_DEV_TYPE_ARM_VGIC_V3)
2. Set distributor address:
   KVM_DEV_ARM_VGIC_GRP_ADDR, KVM_VGIC_V3_ADDR_TYPE_DIST → dist_addr
3. Set redistributor address:
   KVM_DEV_ARM_VGIC_GRP_ADDR, KVM_VGIC_V3_ADDR_TYPE_REDIST → redist_addr
4. Set IRQ count:
   KVM_DEV_ARM_VGIC_GRP_NR_IRQS → 128 (96 SPIs + 32 reserved)
5. Finalize:
   KVM_DEV_ARM_VGIC_GRP_CTRL, KVM_DEV_ARM_VGIC_CTRL_INIT
6. (Optional) Create ITS for MSI:
   KVM_CREATE_DEVICE(KVM_DEV_TYPE_ARM_VGIC_ITS)
   Set ITS address, finalize
```

GIC address calculation:
```
dist_addr    = MMIO32_MEM_START - 64K          (below 1GiB)
redist_addr  = dist_addr - (vcpu_count * 128K)
its_addr     = redist_addr - 128K
```

## Step 2: FDT (Flattened Device Tree)

Source: `fdt.rs` — Uses `vm-fdt` crate (Go: use `github.com/skmcgrail/go-dtb` or build manually).

Required FDT nodes:
```
/ {
    compatible = "linux,dummy-virt";
    #address-cells = <2>;
    #size-cells = <2>;
    interrupt-parent = <&gic>;

    cpus {
        #address-cells = <2>;
        #size-cells = <0>;
        cpu@0 {
            device_type = "cpu";
            compatible = "arm,arm-v8";
            enable-method = "psci";
            reg = <mpidr & 0x7FFFFF>;
        }
    }

    memory@DRAM_START {
        device_type = "memory";
        reg = <DRAM_START, DRAM_SIZE>;
    }

    chosen {
        bootargs = "console=ttyAMA0 ...";
        linux,initrd-start = <initrd_addr>;
        linux,initrd-end = <initrd_addr + initrd_size>;
    }

    intc: interrupt-controller@DIST_ADDR {
        compatible = "arm,gic-v3";
        #interrupt-cells = <3>;
        interrupt-controller;
        reg = <dist_addr dist_size redist_addr redist_size>;
    }

    timer {
        compatible = "arm,armv8-timer";
        interrupts = <PPI 13 edge_rising>,  // secure phys
                     <PPI 14 edge_rising>,  // non-secure phys
                     <PPI 11 edge_rising>,  // virtual
                     <PPI 10 edge_rising>;  // hypervisor
        always-on;
    }

    psci {
        compatible = "arm,psci-0.2";
        method = "hvm";  // or "smc"
    }

    // One node per virtio-mmio device:
    virtio_mmio@ADDR {
        compatible = "virtio,mmio";
        reg = <ADDR 0x1000>;
        interrupts = <SPI irq_num level_hi>;
    }
}
```

## Step 3: vCPU Register Setup

Source: `vcpu.rs`, `regs.rs`

After loading kernel and FDT:
```
PC     = kernel_entry_address (from PE loader or known offset)
X0     = FDT address (dtb pointer)
PSTATE = PSR_MODE_EL1h | PSR_A_BIT | PSR_F_BIT | PSR_I_BIT | PSR_D_BIT
         = 0x3C5
```

Register IDs (KVM encoding):
```
PC       = KVM_REG_ARM64 | KVM_REG_SIZE_U64 | KVM_REG_ARM_CORE | (offset_of(pc) / 4)
PSTATE   = similar encoding with pstate offset
X0       = similar with regs[0] offset
MPIDR_EL1 = arm64_sys_reg(3, 0, 0, 0, 5)  — read to get CPU affinity for FDT
```

## Step 4: vCPU Run Loop

Source: `vstate/vcpu.rs`

```
loop {
    kvm_run(vcpu_fd)
    switch exit_reason:
        case KVM_EXIT_MMIO:
            if is_write:
                mmio_bus.write(addr, data)
            else:
                mmio_bus.read(addr, data)
        case KVM_EXIT_SYSTEM_EVENT:
            if SHUTDOWN: stop VM
            if RESET: reset VM
        case KVM_EXIT_FAIL_ENTRY:
            fatal error
        case KVM_EXIT_INTERNAL_ERROR:
            fatal error
}
```

MMIO exit handling is the key dispatcher — all virtio devices are memory-mapped.
Each device gets a 4K MMIO window. The bus routes reads/writes to the correct device.

## Step 5: Virtio MMIO Transport

Source: `devices/virtio/transport/mmio.rs`

Virtio-MMIO registers (offset from device base):
```
0x000  MagicValue    (R)   0x74726976 ("virt")
0x004  Version       (R)   2 (modern)
0x008  DeviceID      (R)   device type
0x00C  VendorID      (R)
0x010  DeviceFeatures(R)   feature bits
0x014  DeviceFeaturesSel(W)
0x020  DriverFeatures(W)
0x024  DriverFeaturesSel(W)
0x030  QueueSel      (W)   select virtqueue
0x034  QueueNumMax   (R)   max queue size
0x038  QueueNum      (W)   current queue size
0x044  QueueReady    (W)   1 = queue enabled
0x050  QueueNotify   (W)   notify device
0x060  InterruptStatus(R)
0x064  InterruptACK  (W)
0x070  Status        (RW)  driver↔device handshake
0x080  QueueDescLow  (W)
0x084  QueueDescHigh (W)
0x090  QueueDriverLow(W)
0x094  QueueDriverHigh(W)
0x0A0  QueueDeviceLow(W)
0x0A4  QueueDeviceHigh(W)
0x100+ Config space   (RW)  device-specific
```

## Mapping to Your Go Code

| Firecracker (Rust) | Your Go equivalent |
|--------------------|--------------------|
| `arch/aarch64/layout.rs` | `pkg/lokavm/kvm_layout.go` (constants) |
| `arch/aarch64/gic/gicv3/mod.rs` | `pkg/lokavm/kvm_gic.go` (GIC setup via ioctls) |
| `arch/aarch64/fdt.rs` | `pkg/lokavm/kvm_fdt.go` (DTB generation) |
| `arch/aarch64/vcpu.rs` | `pkg/lokavm/kvm_vcpu.go` (register setup) |
| `arch/aarch64/regs.rs` | `pkg/lokavm/kvm_regs.go` (register constants) |
| `vstate/vcpu.rs` (run loop) | `pkg/lokavm/kvm_linux.go` (MMIO dispatch) |
| `devices/virtio/transport/mmio.rs` | `pkg/lokavm/virtio/mmio.go` (MMIO transport) |
| `vstate/bus.rs` | `pkg/lokavm/kvm_bus.go` (MMIO address routing) |

## Go Libraries to Use

- **KVM ioctls**: `golang.org/x/sys/unix` — use `unix.IoctlSetInt`, `unix.Mmap`, raw syscalls
- **FDT generation**: hand-build DTB blob (it's a simple binary format) or use a Go FDT library
- **kvm-bindings equivalent**: define constants manually from Linux headers

## Key KVM ioctls Needed

```go
KVM_CREATE_VM           = 0xAE01
KVM_CREATE_VCPU         = 0xAE41
KVM_SET_USER_MEMORY_REGION = 0x4020AE46
KVM_RUN                 = 0xAE80
KVM_GET_ONE_REG         = 0x4010AEAB
KVM_SET_ONE_REG         = 0x4010AEAC
KVM_CREATE_DEVICE       = 0xC00CAEE0
KVM_SET_DEVICE_ATTR     = 0x4018AEE1
KVM_ARM_PREFERRED_TARGET = 0x8020AEAF
KVM_ARM_VCPU_INIT       = 0x4020AEAE
```

## Estimated LOC for Go Implementation

| Component | LOC | Notes |
|-----------|-----|-------|
| KVM ioctls wrapper | ~200 | Open /dev/kvm, create VM/vCPU, mmap memory |
| GIC setup | ~150 | KVM_CREATE_DEVICE + device attrs |
| FDT generation | ~300 | Build DTB blob for cpus, memory, devices, timer |
| vCPU register setup | ~100 | Set PC, X0, PSTATE via KVM_SET_ONE_REG |
| MMIO bus + dispatch | ~200 | Route MMIO exits to virtio devices |
| Virtio MMIO transport | ~300 | Register reads/writes, queue setup |
| vCPU run loop | ~150 | KVM_RUN + exit handling |
| **Total** | **~1400** | Plus your existing virtio devices |
