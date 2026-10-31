use zed_extension_api as zed;

/// The goi extension ships a grammar and starts the `goipls` language server.
///
/// The server is a Go binary rather than a wasm module, so it is not bundled
/// with the extension: it must be installed and on PATH. Returning a clear
/// error beats starting nothing, because Zed shows it in the language server
/// menu.
struct GoiExtension;

const INSTALL_HINT: &str =
    "goipls not found on PATH. Install it with `go install github.com/ksckaan1/goi/cmd/goipls@latest`";

impl zed::Extension for GoiExtension {
    fn new() -> Self {
        Self
    }

    fn language_server_command(
        &mut self,
        _language_server_id: &zed::LanguageServerId,
        worktree: &zed::Worktree,
    ) -> zed::Result<zed::Command> {
        let command = worktree.which("goipls").ok_or_else(|| INSTALL_HINT.to_string())?;
        Ok(zed::Command {
            command,
            args: vec![],
            env: vec![],
        })
    }
}

zed::register_extension!(GoiExtension);
