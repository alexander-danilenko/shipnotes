package notes

// Renderer is the port for turning the shipnotes model into its final text
// form. The domain defines the contract; the markdown adapter in the
// infrastructure layer implements it with text/template. Keeping it behind a
// port means the orchestration layer depends on the idea of "render", not on a
// specific template engine.
type Renderer interface {
	Render(data ReleaseNotes) (string, error)
}
