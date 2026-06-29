package autoscaler

// behavior.go holds the hand-written behaviour over the generated contract enums
// (DecisionKind, ReasonCode). Per the schema-first contract rule, the generated
// contract types carry NO methods — behaviour the generator cannot produce lives
// here as FREE FUNCTIONS that take the enum value as a parameter. The enum consts
// (DecisionNoChange, ReasonCPUHigh, …) are the generated contract surface
// (contract.gen.go); these functions reference them by name.
