package policy

// EffectKind identifies the category of an effect a principal wants to
// perform against a scope. Stage 1 does not enumerate a closed set here on
// purpose: harnesses and the plan model define concrete kinds (file_write,
// pr_create, memory_propose, and so on); the kernel only needs kinds to be
// comparable values, never their meaning.
type EffectKind string

// Effect is a single action, scoped, that a principal wants the kernel to
// authorize. Target is resource-specific (a path, an API route, an issue
// key) and is opaque to the kernel; it exists so the audit reason can name
// the thing an effect touched, not so the kernel can reason about it.
type Effect struct {
	Kind   EffectKind
	Scope  ScopeID
	Target string
}
