use zed_extension_api as zed;

/// The goi extension is grammar-only: it has no language server and no
/// commands, so the default trait implementations are all it needs.
struct GoiExtension;

impl zed::Extension for GoiExtension {
    fn new() -> Self {
        Self
    }
}

zed::register_extension!(GoiExtension);
