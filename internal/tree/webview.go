package tree

// WebView is a native browser view embedded in the window. The platform paints
// it above the GPU surface; the node itself only tells the ui layer where it
// belongs and carries the properties the view loads.
type WebView struct {
	Component
	Src       Prop[string]
	HTML      Prop[string]
	BaseURL   Prop[string]
	UserAgent Prop[string]
	Zoom      Prop[float64]
	Devtools  Prop[bool]

	// ctrl is installed by the ui layer once the native view exists. A design
	// can call the methods below from Go before then; they are no-ops until the
	// view is ready.
	ctrl      WebviewController
	onMessage func(string)
	// pendingFocus asks the ui layer to focus the view as soon as it exists; a
	// design can call Focus before the native view is created.
	pendingFocus bool
	// RenderedSrc and RenderedHTML are the values the native view last loaded,
	// so the ui layer can tell a changed src from one already shown.
	RenderedSrc  string
	RenderedHTML string
	// Loaded is true once the platform reported the first successful load.
	Loaded bool
	// LoadErr is the platform's last load failure.
	LoadErr error
}

// WebviewController is the part of the native view the node forwards its Go
// methods to. The ui layer implements it; tree stays free of platform imports.
type WebviewController interface {
	Reload()
	GoBack() bool
	GoForward() bool
	Eval(js string)
	Focus()
}

// Kind implements Node.
func (*WebView) Kind() string { return "WebView" }

func (c *WebView) namedProps() []NamedProp {
	b := &c.NodeBase
	return append(c.propsExcept("WebView",
		"src", "html", "baseURL", "userAgent", "zoom", "devtools"),
		LayoutStringProp(b, "src", &c.Src),
		LayoutStringProp(b, "html", &c.HTML),
		LayoutStringProp(b, "baseURL", &c.BaseURL),
		StringProp(b, "userAgent", &c.UserAgent),
		NumberProp(b, "zoom", DirtyPaint, &c.Zoom),
		BoolProp(b, "devtools", DirtyPaint, &c.Devtools),
	)
}

// OnMessage sets the function that receives messages the page posts through
// window.webkit.messageHandlers.goi.postMessage (Linux) or the platform's
// equivalent bridge.
func (c *WebView) OnMessage(fn func(string)) { c.onMessage = fn }

// DeliverMessage is called by the ui layer when the page posts a message.
func (c *WebView) DeliverMessage(msg string) {
	if c.onMessage != nil {
		c.onMessage(msg)
	}
}

// SetController installs the native view. The ui layer calls it when the view
// is created or recreated.
func (c *WebView) SetController(ctrl WebviewController) { c.ctrl = ctrl }

// Load points the view at a URL.
func (c *WebView) Load(src string) { c.Src.Set(src) }

// SetHTML replaces the view with inline HTML, resolved against base.
func (c *WebView) SetHTML(html, base string) {
	c.HTML.Set(html)
	c.BaseURL.Set(base)
}

// Reload reloads the current page.
func (c *WebView) Reload() {
	if c.ctrl != nil {
		c.ctrl.Reload()
	}
}

// GoBack navigates back and reports whether the view could.
func (c *WebView) GoBack() bool {
	return c.ctrl != nil && c.ctrl.GoBack()
}

// GoForward navigates forward and reports whether the view could.
func (c *WebView) GoForward() bool {
	return c.ctrl != nil && c.ctrl.GoForward()
}

// Eval runs JavaScript in the page.
func (c *WebView) Eval(js string) {
	if c.ctrl != nil {
		c.ctrl.Eval(js)
	}
}

// Focus gives the page the keyboard and makes it the target of forwarded keys.
// It is safe before the native view exists; the ui layer applies it when the
// view is created.
func (c *WebView) Focus() {
	c.pendingFocus = true
	if c.ctrl != nil {
		c.ctrl.Focus()
	}
}

// TakeFocus reports and clears a focus request made before the view existed.
func (c *WebView) TakeFocus() bool {
	on := c.pendingFocus
	c.pendingFocus = false
	return on
}
