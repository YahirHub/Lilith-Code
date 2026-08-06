package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/interaction"
	"github.com/lilith/li/internal/sshremote"
)

var sshActions = []string{"connect", "connect_server", "list_servers", "get_server", "add_server", "update_server", "rename_server", "delete_server", "vault_status", "unlock_vault", "lock_vault", "change_vault_password", "set_server_password", "clear_server_password", "set_server_passphrase", "clear_server_passphrase", "list_connections", "status", "pwd", "cd", "list", "stat", "read_file", "exec", "shell_open", "shell_write", "shell_read", "upload", "download", "write_file", "mkdir", "rename", "delete", "close", "close_all"}

func init() {
	register(Definition{
		Name:          "ssh_remote",
		Description:   "Persistent SSH manager with reusable non-secret server profiles, stable logical connection IDs, automatic transport recovery, SFTP, remote commands/shells, and an AES-256-GCM encrypted credential vault. The vault opens lazily only when a remote action needs a saved credential, then remains open for that process. Secret prompts appear locally inside the chat and are never sent to the model. Approval policy is configured under /config > Seguridad > SSH Remoto.",
		PromptSnippet: "Manage saved SSH servers, encrypted credentials and persistent remote connections",
		PromptGuidelines: []string{
			"Use list_servers before connecting when the user may already have a saved server; use connect_server with the returned reference.",
			"Prefer prompt_password/prompt_passphrase or environment/key references. Never ask the user to paste a password into chat.",
			"Use one persistent connection_id for related remote operations and close it when it is no longer needed.",
			"Omit timeout_seconds for long remote builds, deployments and installations; SSH exec has no artificial deadline unless one is explicitly requested.",
			"Remote file actions use privilege_mode=auto by default: they try SFTP first and safely fall back to root, sudo or passwordless doas for protected paths. Use privilege_mode=never to forbid elevation or required when elevation is known to be necessary.",
			"For an arbitrary exec command, elevation is never retried automatically after a permission error because the command may already have partial effects. Set privilege_mode=required explicitly when the complete command must run through sudo/root/doas.",
			"A connection_id is a stable logical connection. If its transport drops, call the next action with the same ID; Lilith reconnects automatically. Never close and reconnect merely because an exec result reports transport_recovered or an unknown exit status.",
			"Respect the user's SSH approval policy; do not request a second conversational confirmation before calling this tool.",
			"Never read or return protected .env files unless the user explicitly authorizes that specific operation.",
		},
		Parameters: sshRemoteSchema(), Mutating: true,
		Available: func(env Env) bool { return strings.TrimSpace(env.ConfigDir) != "" },
		Run:       runSSHRemote,
	})
}

func sshRemoteSchema() map[string]any {
	properties := map[string]any{
		"action": map[string]any{"type": "string", "enum": sshActions}, "connection_id": map[string]any{"type": "string", "minLength": 1}, "server_id": map[string]any{"type": "string", "minLength": 1}, "shell_id": map[string]any{"type": "string", "minLength": 1}, "name": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}, "new_name": map[string]any{"type": "string"}, "clear_name": map[string]any{"type": "boolean"}, "clear_authentication": map[string]any{"type": "boolean"}, "close_connections": map[string]any{"type": "boolean"}, "prompt_password": map[string]any{"type": "boolean"}, "prompt_passphrase": map[string]any{"type": "boolean"}, "save_server": map[string]any{"type": "boolean"}, "host": map[string]any{"type": "string"}, "port": map[string]any{"type": "integer", "minimum": 1, "maximum": 65535}, "username": map[string]any{"type": "string"}, "password": map[string]any{"type": "string", "description": "Ephemeral only. Prefer prompt_password; never persist this field."}, "password_env": map[string]any{"type": "string"}, "private_key_path": map[string]any{"type": "string"}, "private_key": map[string]any{"type": "string", "description": "Ephemeral key contents; never persisted."}, "passphrase": map[string]any{"type": "string"}, "passphrase_env": map[string]any{"type": "string"}, "agent": map[string]any{"type": "string"}, "agent_env": map[string]any{"type": "string"}, "host_fingerprint_sha256": map[string]any{"type": "string"}, "ready_timeout_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 120000}, "keepalive_interval_ms": map[string]any{"type": "integer", "minimum": 1000, "maximum": 120000}, "path": map[string]any{"type": "string"}, "destination_path": map[string]any{"type": "string"}, "local_path": map[string]any{"type": "string"}, "remote_path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}, "encoding": map[string]any{"type": "string", "enum": []string{"utf8", "base64"}}, "command": map[string]any{"type": "string", "minLength": 1}, "timeout_seconds": map[string]any{"type": "number", "minimum": -1, "maximum": 86400, "description": "Optional hard deadline. Omit it or use -1 for long builds/deployments without an artificial timeout."}, "pty": map[string]any{"type": "boolean"}, "cols": map[string]any{"type": "integer", "minimum": 20, "maximum": 500}, "rows": map[string]any{"type": "integer", "minimum": 5, "maximum": 200}, "wait_ms": map[string]any{"type": "integer", "minimum": 0, "maximum": 30000}, "max_bytes": map[string]any{"type": "integer", "minimum": 1, "maximum": 10485760}, "recursive": map[string]any{"type": "boolean"}, "overwrite": map[string]any{"type": "boolean"}, "privilege_mode": map[string]any{"type": "string", "enum": []string{"auto", "never", "required"}, "description": "Privilege policy. File operations: auto tries normal SFTP first and safely falls back to root/sudo/doas. exec: required elevates the complete command; auto never retries an arbitrary command after it started. never forbids elevation."}, "reason": map[string]any{"type": "string"},
	}
	serverActions := []string{"connect_server", "get_server", "update_server", "rename_server", "delete_server", "set_server_password", "clear_server_password", "set_server_passphrase", "clear_server_passphrase"}
	connectionActions := []string{"status", "pwd", "cd", "list", "stat", "read_file", "exec", "shell_open", "shell_write", "shell_read", "upload", "download", "write_file", "mkdir", "rename", "delete", "close"}
	conditions := []any{
		sshSchemaCondition([]string{"connect"}, []string{"host", "username"}, []any{
			map[string]any{"required": []string{"password"}}, map[string]any{"required": []string{"password_env"}},
			map[string]any{"required": []string{"private_key"}}, map[string]any{"required": []string{"private_key_path"}},
			map[string]any{"required": []string{"agent"}}, map[string]any{"required": []string{"agent_env"}},
			map[string]any{"properties": map[string]any{"prompt_password": map[string]any{"const": true}}, "required": []string{"prompt_password"}},
		}),
		sshSchemaCondition([]string{"add_server"}, []string{"host", "username"}, nil),
		sshSchemaCondition(serverActions, []string{"server_id"}, nil),
		sshSchemaCondition(connectionActions, []string{"connection_id"}, nil),
		sshSchemaCondition([]string{"cd", "stat", "read_file", "write_file", "mkdir", "delete"}, []string{"path"}, nil),
		sshSchemaCondition([]string{"rename"}, []string{"path", "destination_path"}, nil),
		sshSchemaCondition([]string{"upload", "download"}, []string{"local_path", "remote_path"}, nil),
		sshSchemaCondition([]string{"exec", "shell_write"}, []string{"command"}, nil),
	}
	return map[string]any{"type": "object", "properties": properties, "required": []string{"action"}, "allOf": conditions}
}

func sshSchemaCondition(actions, required []string, anyOf []any) map[string]any {
	then := map[string]any{}
	if len(required) > 0 {
		then["required"] = required
	}
	if len(anyOf) > 0 {
		then["anyOf"] = anyOf
	}
	return map[string]any{
		"if":   map[string]any{"properties": map[string]any{"action": map[string]any{"enum": actions}}, "required": []string{"action"}},
		"then": then,
	}
}
func runSSHRemote(ctx context.Context, args map[string]any, env Env) (string, error) {
	action := strings.TrimSpace(str(args, "action"))
	if action == "" {
		return "", errors.New("action is required")
	}
	manager := sshremote.GetManager(env.ConfigDir, env.RequestSecret, env.Confirm)
	settings, _ := config.Load(env.ConfigDir)
	permission, permissionLabel := sshPermissionForAction(action)
	rule := sshApprovalRule(action, args, manager)
	if config.SSHApprovalRequired(settings, permission) && !config.HasSSHProjectApproval(settings, env.Root, rule) {
		message := sshApprovalMessage(action, permissionLabel, args)
		if env.Approve != nil {
			key := "ssh|" + filepath.Clean(env.Root) + "|" + rule
			decision, err := env.Approve(ctx, "Permiso SSH remoto", message, key)
			if err != nil {
				return "", err
			}
			switch decision {
			case interaction.ApprovalOnce, interaction.ApprovalSession:
			case interaction.ApprovalProject:
				config.AddSSHProjectApproval(&settings, env.Root, rule)
				if err := config.Save(env.ConfigDir, settings); err != nil {
					return "", fmt.Errorf("guardar permiso SSH del proyecto: %w", err)
				}
			default:
				return "", errors.New("acción SSH rechazada por el usuario")
			}
		} else {
			if env.Confirm == nil {
				return "", errors.New("la política SSH requiere aprobación local")
			}
			ok, err := env.Confirm(ctx, "Permiso SSH remoto", message)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", errors.New("acción SSH rechazada por el usuario")
			}
		}
	}
	switch action {
	case "list_servers":
		profiles, err := manager.Store().List()
		if err != nil {
			return "", err
		}
		status, _ := manager.Vault().Status()
		activeUnconfigured := []map[string]any{}
		for _, connection := range manager.ListConnections() {
			if fmt.Sprint(connection["server_id"]) != "" {
				continue
			}
			activeUnconfigured = append(activeUnconfigured, map[string]any{
				"name": connection["name"], "host": connection["host"], "port": connection["port"],
				"username": connection["username"], "persistent": false, "connected": true,
				"connection_id": connection["connection_id"], "connection_ref": connection["connection_ref"],
			})
		}
		message := "No se encontraron servidores SSH configurados. Usa add_server o connect con save_server=true."
		if len(profiles) > 0 {
			message = "Servidores SSH configurados cargados desde el registro global de Lilith."
		}
		return jsonOutput(map[string]any{
			"action": "list_servers", "servers": compactProfilesWithManager(manager, profiles),
			"configured_count": len(profiles), "registry_path": manager.Store().Path(),
			"credential_vault": status, "active_unconfigured_servers": activeUnconfigured,
			"active_unconfigured_count": len(activeUnconfigured), "message": message,
		})
	case "get_server":
		p, err := manager.Store().Get(requiredArg(args, "server_id"))
		if err != nil {
			return "", err
		}
		status, _ := manager.Vault().Status()
		out := compactProfileWithManager(manager, p)
		out["action"] = "get_server"
		out["credential_vault"] = status
		return jsonOutput(out)
	case "add_server":
		return addSSHServer(ctx, args, env, manager)
	case "update_server":
		return updateSSHServer(ctx, args, env, manager)
	case "rename_server":
		clear := boolArgOr(args, "clear_name", false)
		name := firstNonEmpty(str(args, "new_name"), str(args, "name"), str(args, "label"))
		if !clear && name == "" {
			return "", errors.New("rename_server requiere new_name, name o label; usa clear_name=true para eliminar el nombre personalizado")
		}
		p, err := manager.Store().Rename(requiredArg(args, "server_id"), name, clear)
		if err != nil {
			return "", err
		}
		manager.RenameServerConnections(p.ID, sshremote.DisplayName(p))
		out := compactProfileWithManager(manager, p)
		out["ok"] = true
		out["action"] = "rename_server"
		if p.Name == "" {
			out["message"] = "Nombre personalizado eliminado; Lilith vuelve a mostrar usuario@host."
		} else {
			out["message"] = "Servidor SSH renombrado."
		}
		return jsonOutput(out)
	case "delete_server":
		ref := requiredArg(args, "server_id")
		existing, err := manager.Store().Get(ref)
		if err != nil {
			return "", err
		}
		encrypted := existing.PasswordVault || existing.PassphraseVault
		if encrypted {
			if _, err = manager.Vault().DeleteServer(ctx, existing.ID); err != nil {
				return "", err
			}
		}
		p, err := manager.Store().Delete(existing.ID)
		if err != nil {
			return "", err
		}
		closed := []string{}
		active := manager.ListConnections()
		if boolArgOr(args, "close_connections", false) {
			for _, c := range active {
				if fmt.Sprint(c["server_id"]) == p.ID {
					closed = append(closed, fmt.Sprint(c["connection_id"]))
				}
			}
			manager.CloseByServer(p.ID)
		} else {
			manager.DetachServer(p.ID)
		}
		kept := 0
		if !boolArgOr(args, "close_connections", false) {
			for _, c := range active {
				if fmt.Sprint(c["server_id"]) == p.ID {
					kept++
				}
			}
		}
		return jsonOutput(map[string]any{
			"ok": true, "action": "delete_server", "deleted_server": compactProfile(p),
			"encrypted_credentials_deleted": encrypted, "closed_connection_ids": closed,
			"active_connections_kept": kept,
			"message": func() string {
				if boolArgOr(args, "close_connections", false) {
					return "Servidor guardado y credenciales cifradas eliminados; las conexiones vinculadas fueron cerradas."
				}
				return "Servidor guardado y credenciales cifradas eliminados; las conexiones existentes siguen abiertas hasta cerrarlas explícitamente."
			}(),
		})
	case "vault_status":
		status, err := manager.Vault().Status()
		if err != nil {
			return "", err
		}
		return jsonOutput(withAction("vault_status", status))
	case "unlock_vault":
		if err := manager.Vault().Unlock(ctx); err != nil {
			return "", err
		}
		status, _ := manager.Vault().Status()
		out := withAction("unlock_vault", status)
		out["ok"] = true
		out["message"] = "La bóveda SSH quedó desbloqueada sólo para esta ejecución de Lilith."
		return jsonOutput(out)
	case "lock_vault":
		manager.Vault().Lock()
		status, _ := manager.Vault().Status()
		out := withAction("lock_vault", status)
		out["ok"] = true
		out["message"] = "La clave de la bóveda SSH fue eliminada de la memoria del proceso."
		return jsonOutput(out)
	case "change_vault_password":
		if err := manager.Vault().ChangePassword(ctx); err != nil {
			return "", err
		}
		status, _ := manager.Vault().Status()
		out := withAction("change_vault_password", status)
		out["ok"] = true
		out["message"] = "La bóveda SSH fue cifrada nuevamente con la nueva contraseña maestra."
		return jsonOutput(out)
	case "set_server_password":
		return setVaultField(ctx, args, env, manager, "password")
	case "set_server_passphrase":
		return setVaultField(ctx, args, env, manager, "passphrase")
	case "clear_server_password":
		return clearVaultField(ctx, args, manager, "password")
	case "clear_server_passphrase":
		return clearVaultField(ctx, args, manager, "passphrase")
	case "connect":
		return directSSHConnect(ctx, args, env, manager)
	case "connect_server":
		return savedSSHConnect(ctx, args, env, manager)
	case "list_connections":
		connections := manager.ListConnections()
		return jsonOutput(map[string]any{"action": "list_connections", "connections": connections, "count": len(connections)})
	case "close_all":
		connections := manager.ListConnections()
		closedIDs := make([]string, 0, len(connections))
		for _, connection := range connections {
			closedIDs = append(closedIDs, fmt.Sprint(connection["connection_id"]))
		}
		manager.CloseAll()
		return jsonOutput(map[string]any{"ok": true, "action": "close_all", "closed_all": true, "closed_connection_ids": closedIDs, "count": len(closedIDs)})
	}
	conn, err := manager.Get(requiredArg(args, "connection_id"))
	if err != nil {
		return "", err
	}
	privilegeMode, err := sshremote.ParsePrivilegeMode(str(args, "privilege_mode"))
	if err != nil {
		return "", err
	}
	unlock := conn.LockOperation()
	defer unlock()
	switch action {
	case "status":
		if err := conn.EnsureConnected(ctx); err != nil {
			return "", err
		}
		out := connectionStatus(conn)
		out["action"] = "status"
		return jsonOutput(out)
	case "pwd":
		cwd, err := conn.Pwd(ctx)
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": "pwd", "connection_id": conn.ID, "cwd": cwd, "path": cwd})
	case "cd":
		cwd, err := conn.CD(ctx, requiredArg(args, "path"))
		if err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"action": "cd", "connection_id": conn.ID, "cwd": cwd, "path": cwd})
	case "list":
		requested := defaultString(str(args, "path"), ".")
		items, privilege, err := conn.ListWithPrivilege(ctx, requested, privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"action": "list", "connection_id": conn.ID, "path": conn.Resolve(requested), "entries": items, "count": len(items)}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "stat":
		info, privilege, err := conn.StatWithPrivilege(ctx, requiredArg(args, "path"), privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"action": "stat", "connection_id": conn.ID, "name": info.Name, "path": info.Path, "size": info.Size, "mode": info.Mode, "modified_at": info.ModifiedAt, "type": info.Type}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "read_file":
		requested := requiredArg(args, "path")
		if settings.ProtectEnvFiles && isProtectedEnv(requested) {
			if err := authorizeProtectedEnv(ctx, env, "Leer archivo .env remoto", requested); err != nil {
				return "", err
			}
		}
		encoding := defaultString(str(args, "encoding"), "utf8")
		maxBytes := intArgOr(args, "max_bytes", 200000)
		content, size, truncated, privilege, err := conn.ReadFileWithPrivilege(ctx, requested, encoding, maxBytes, privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"action": "read_file", "connection_id": conn.ID, "path": conn.Resolve(requested), "size": size, "truncated": truncated, "encoding": encoding, "content": content}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "exec":
		command := requiredArg(args, "command")
		var result sshremote.ExecResult
		var privilege sshremote.PrivilegeInfo
		if privilegeMode == sshremote.PrivilegeRequired {
			result, privilege, err = conn.ExecWithPrivilege(ctx, command, secondsArg(args, "timeout_seconds", -1), boolArgOr(args, "pty", false), intArgOr(args, "cols", 120), intArgOr(args, "rows", 30), privilegeMode)
		} else {
			// An arbitrary command is not safe to repeat after a permission error:
			// it may already have produced partial side effects. Elevation therefore
			// requires an explicit privilege_mode=required before execution.
			result, err = conn.Exec(ctx, command, secondsArg(args, "timeout_seconds", -1), boolArgOr(args, "pty", false), intArgOr(args, "cols", 120), intArgOr(args, "rows", 30))
		}
		stdout, stdoutCut := trimSSHOutput(result.Stdout, intArgOr(args, "max_bytes", 200000))
		stderr, stderrCut := trimSSHOutput(result.Stderr, intArgOr(args, "max_bytes", 200000))
		out := map[string]any{
			"action": "exec", "connection_id": conn.ID, "connection_ref": "ssh://" + conn.ID,
			"connection_id_unchanged": true, "cwd": conn.Snapshot().CWD, "command": command,
			"stdout": stdout, "stderr": stderr, "stdout_truncated": result.StdoutTruncated || stdoutCut,
			"stderr_truncated": result.StderrTruncated || stderrCut, "exitCode": result.ExitCode,
			"exit_code": result.ExitCode, "exit_status_known": result.ExitStatusKnown, "signal": nil,
			"timed_out": result.TimedOut, "duration_ms": result.DurationMS,
			"transport_recovered": result.TransportRecovered, "reconnect_count": result.ReconnectCount,
			"connection_generation": result.ConnectionGeneration,
		}
		addSSHPrivilegeResult(out, privilege)
		if result.TransportNotice != "" {
			out["transport_notice"] = result.TransportNotice
			out["message"] = "La conexión lógica sigue disponible con el mismo connection_id. No cierres ni reconectes manualmente; verifica sólo el estado del comando porque el servidor no confirmó su código de salida."
		}
		if privilegeMode == sshremote.PrivilegeAuto && sshremote.IsPermissionDenied(errors.New(result.Stderr+"\n"+result.Stdout)) {
			out["privilege_hint"] = "El comando terminó con acceso denegado. Si todo el comando debe ejecutarse elevado, reintenta explícitamente con privilege_mode=required; Lilith no repite automáticamente comandos arbitrarios que pudieron tener efectos parciales."
		}
		text, _ := jsonOutput(out)
		return text, err
	case "upload":
		localPath := resolveTransferPath(requiredArg(args, "local_path"), env.Root)
		remotePath := conn.Resolve(requiredArg(args, "remote_path"))
		privilege, err := conn.UploadWithPrivilege(ctx, localPath, remotePath, boolArgOr(args, "overwrite", false), privilegeMode)
		if err != nil {
			return "", err
		}
		info, _ := os.Stat(localPath)
		out := map[string]any{"ok": true, "action": "upload", "uploaded": true, "connection_id": conn.ID, "local_path": localPath, "remote_path": remotePath, "bytes": fileSize(info)}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "download":
		remotePath := conn.Resolve(requiredArg(args, "remote_path"))
		localPath := resolveTransferPath(requiredArg(args, "local_path"), env.Root)
		privilege, err := conn.DownloadWithPrivilege(ctx, remotePath, localPath, boolArgOr(args, "overwrite", false), privilegeMode)
		if err != nil {
			return "", err
		}
		info, _ := os.Stat(localPath)
		out := map[string]any{"ok": true, "action": "download", "downloaded": true, "connection_id": conn.ID, "remote_path": remotePath, "local_path": localPath, "bytes": fileSize(info)}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "write_file":
		requested := requiredArg(args, "path")
		if isProtectedEnv(requested) && settings.ProtectEnvFiles {
			if err := authorizeProtectedEnv(ctx, env, "Escribir archivo .env remoto", requested); err != nil {
				return "", err
			}
		}
		if _, ok := args["content"]; !ok {
			return "", errors.New("content is required")
		}
		encoding := defaultString(str(args, "encoding"), "utf8")
		content := str(args, "content")
		privilege, err := conn.WriteFileWithPrivilege(ctx, requested, content, encoding, boolArgOr(args, "overwrite", false), privilegeMode)
		if err != nil {
			return "", err
		}
		bytesWritten, err := encodedLength(content, encoding)
		if err != nil {
			return "", err
		}
		out := map[string]any{"ok": true, "action": "write_file", "written": true, "connection_id": conn.ID, "path": conn.Resolve(requested), "bytes": bytesWritten}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "mkdir":
		requested := requiredArg(args, "path")
		privilege, err := conn.MkdirWithPrivilege(ctx, requested, boolArgOr(args, "recursive", false), privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"ok": true, "action": "mkdir", "created": true, "connection_id": conn.ID, "path": conn.Resolve(requested)}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "rename":
		source := requiredArg(args, "path")
		destination := requiredArg(args, "destination_path")
		privilege, err := conn.RenameWithPrivilege(ctx, source, destination, boolArgOr(args, "overwrite", false), privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"ok": true, "action": "rename", "renamed": true, "connection_id": conn.ID, "path": conn.Resolve(source), "destination_path": conn.Resolve(destination), "from": conn.Resolve(source), "to": conn.Resolve(destination)}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "delete":
		requested := requiredArg(args, "path")
		recursive := boolArgOr(args, "recursive", false)
		privilege, err := conn.DeleteWithPrivilege(ctx, requested, recursive, privilegeMode)
		if err != nil {
			return "", err
		}
		out := map[string]any{"ok": true, "action": "delete", "deleted": true, "connection_id": conn.ID, "path": conn.Resolve(requested), "recursive": recursive}
		addSSHPrivilegeResult(out, privilege)
		return jsonOutput(out)
	case "shell_open":
		if existing, shellErr := resolveShell(conn, str(args, "shell_id")); shellErr == nil && existing.IsOpen() {
			if err := waitSSH(ctx, intArgOr(args, "wait_ms", 350)); err != nil {
				return "", err
			}
			out := shellToolResult("shell_open", conn, existing.Read(intArgOr(args, "max_bytes", 200000)))
			out["already_open"] = true
			return jsonOutput(out)
		}
		shell, err := conn.OpenShell(intArgOr(args, "cols", 120), intArgOr(args, "rows", 30), intArgOr(args, "max_bytes", 1048576))
		if err != nil {
			return "", err
		}
		if err = waitSSH(ctx, intArgOr(args, "wait_ms", 350)); err != nil {
			return "", err
		}
		return jsonOutput(shellToolResult("shell_open", conn, shell.Read(intArgOr(args, "max_bytes", 200000))))
	case "shell_write":
		shell, err := resolveShell(conn, str(args, "shell_id"))
		if err != nil {
			return "", err
		}
		command := requiredArg(args, "command")
		if err = shell.Write(command + "\n"); err != nil {
			return "", err
		}
		if err = waitSSH(ctx, intArgOr(args, "wait_ms", 350)); err != nil {
			return "", err
		}
		out := shellToolResult("shell_write", conn, shell.Read(intArgOr(args, "max_bytes", 200000)))
		out["command"] = command
		return jsonOutput(out)
	case "shell_read":
		shell, err := resolveShell(conn, str(args, "shell_id"))
		if err != nil {
			return "", err
		}
		if err = waitSSH(ctx, intArgOr(args, "wait_ms", 350)); err != nil {
			return "", err
		}
		return jsonOutput(shellToolResult("shell_read", conn, shell.Read(intArgOr(args, "max_bytes", 200000))))
	case "close":
		id := conn.ID
		if err := manager.Close(id); err != nil {
			return "", err
		}
		return jsonOutput(map[string]any{"ok": true, "action": "close", "closed": true, "connection_id": id, "message": "Conexión SSH cerrada."})
	}
	return "", fmt.Errorf("unsupported ssh_remote action: %s", action)
}

func directSSHConnect(ctx context.Context, args map[string]any, env Env, m *sshremote.Manager) (string, error) {
	host, user := requiredArg(args, "host"), requiredArg(args, "username")
	if host == "" || user == "" {
		return "", errors.New("host and username are required for connect")
	}
	save := boolArgOr(args, "save_server", true)
	password := str(args, "password")
	passphrase := str(args, "passphrase")
	promptPassword := boolArgOr(args, "prompt_password", false) && password == ""
	promptPassphrase := boolArgOr(args, "prompt_passphrase", false) && passphrase == ""
	if promptPassword {
		if env.RequestSecret == nil {
			return "", errors.New("entrada secreta local no disponible")
		}
		v, err := env.RequestSecret(ctx, interaction.SecretRemotePassword, "Contraseña del servidor remoto", "Escribe la contraseña de la cuenta SSH "+user+"@"+host+". Esta es la contraseña del servidor remoto, no la contraseña maestra de la bóveda SSH. No se enviará al modelo ni se guardará en el historial.", false, 1)
		if err != nil {
			return "", err
		}
		password = v
	}
	if promptPassphrase {
		if env.RequestSecret == nil {
			return "", errors.New("entrada secreta local no disponible")
		}
		v, err := env.RequestSecret(ctx, interaction.SecretKeyPassphrase, "Passphrase de la clave privada SSH", "Escribe la passphrase que protege la clave privada SSH. No es la contraseña maestra de la bóveda ni la contraseña de la cuenta del servidor. No se enviará al modelo ni se guardará en el historial.", false, 1)
		if err != nil {
			return "", err
		}
		passphrase = v
	}
	defer func() { password, passphrase = "", "" }()
	opt := connectOptions(args, env.Root)
	opt.Host, opt.Username, opt.Password, opt.Passphrase = host, user, password, passphrase
	opt.PromptPassword, opt.PromptPassphrase = false, false
	conn, err := m.Connect(ctx, opt)
	if err != nil {
		return "", err
	}

	out := map[string]any{
		"ok": true, "action": "connect", "connection": connectionStatus(conn),
		"server_saved": false,
		"message":      "Conexión SSH persistente abierta sin guardar un perfil de servidor.",
	}
	if !save {
		return jsonOutput(out)
	}

	profile, saveErr := m.Store().Add(serverInput(args, env.Root))
	if saveErr != nil {
		out["server_save_error"] = saveErr.Error()
		out["message"] = "La conexión SSH quedó abierta, pero el perfil del servidor no pudo guardarse."
		return jsonOutput(out)
	}
	conn.AttachServer(profile.ID, sshremote.DisplayName(profile))
	out["server_saved"] = true
	out["server_created"] = true
	out["server"] = compactProfile(profile)
	out["connection"] = connectionStatus(conn)

	var credentialErr error
	if promptPassword || promptPassphrase {
		var pw, pp *string
		if promptPassword {
			pw = &password
		}
		if promptPassphrase {
			pp = &passphrase
		}
		if credentialErr = m.Vault().Set(ctx, profile.ID, pw, pp); credentialErr == nil {
			profile, credentialErr = m.Store().Update(profile.ID, sshremote.ServerPatch{ServerInput: sshremote.ServerInput{PasswordVault: promptPassword, PassphraseVault: promptPassphrase}})
		}
		if credentialErr == nil {
			out["prompted_credentials_saved"] = true
			out["server"] = compactProfile(profile)
		} else {
			out["prompted_credentials_saved"] = false
			out["credential_save_error"] = credentialErr.Error()
		}
	}
	if credentialErr != nil {
		out["message"] = "La conexión SSH quedó abierta y el perfil fue guardado, pero la credencial solicitada no pudo cifrarse."
	} else {
		out["message"] = "Conexión SSH persistente abierta y vinculada al registro global de servidores."
	}
	return jsonOutput(out)
}

func savedSSHConnect(ctx context.Context, args map[string]any, env Env, m *sshremote.Manager) (string, error) {
	p, err := m.Store().Get(requiredArg(args, "server_id"))
	if err != nil {
		return "", err
	}
	opt := connectOptions(args, env.Root)
	if _, ok := args["port"]; !ok {
		opt.Port = 0
	}
	if _, ok := args["ready_timeout_ms"]; !ok {
		opt.ReadyTimeoutMS = 0
	}
	if _, ok := args["keepalive_interval_ms"]; !ok {
		opt.KeepaliveIntervalMS = 0
	}
	opt.Profile = &p

	hasAuthOverride := hasAnyArg(args, "password", "password_env", "private_key", "private_key_path", "agent", "agent_env", "prompt_password")
	opt.OverrideAuthentication = hasAuthOverride
	profileHasAuth := p.PasswordEnv != "" || p.PasswordVault || p.PrivateKeyPath != "" || p.Agent != "" || p.AgentEnv != ""
	promptPassword := boolArgOr(args, "prompt_password", false) || (!hasAuthOverride && !profileHasAuth)
	promptPassphrase := boolArgOr(args, "prompt_passphrase", false)
	promptedPassword, promptedPassphrase := "", ""
	if promptPassword {
		if env.RequestSecret == nil {
			return "", errors.New("entrada secreta local no disponible")
		}
		promptedPassword, err = env.RequestSecret(ctx, interaction.SecretRemotePassword, "Contraseña del servidor remoto", "Escribe la contraseña de la cuenta SSH "+sshremote.DisplayName(p)+". Esta es la contraseña del servidor remoto, no la contraseña maestra de la bóveda. Después de conectar se guardará cifrada sólo si así se solicitó.", false, 1)
		if err != nil {
			return "", err
		}
		opt.Password = promptedPassword
	}
	if promptPassphrase {
		if env.RequestSecret == nil {
			return "", errors.New("entrada secreta local no disponible")
		}
		promptedPassphrase, err = env.RequestSecret(ctx, interaction.SecretKeyPassphrase, "Passphrase de la clave privada SSH", "Escribe la passphrase que protege la clave privada de "+sshremote.DisplayName(p)+". No es la contraseña maestra de la bóveda ni la contraseña del servidor. Después de conectar se guardará cifrada.", false, 1)
		if err != nil {
			return "", err
		}
		opt.Passphrase = promptedPassphrase
	}
	defer func() { promptedPassword, promptedPassphrase = "", "" }()
	opt.PromptPassword, opt.PromptPassphrase = false, false
	conn, err := m.Connect(ctx, opt)
	if err != nil {
		return "", err
	}

	out := map[string]any{
		"ok": true, "action": "connect_server", "connection": connectionStatus(conn),
		"server": compactProfile(p), "message": "Conexión SSH persistente abierta y vinculada al registro global de servidores.",
	}
	if promptedPassword == "" && promptedPassphrase == "" {
		return jsonOutput(out)
	}
	var pw, pp *string
	if promptedPassword != "" {
		pw = &promptedPassword
	}
	if promptedPassphrase != "" {
		pp = &promptedPassphrase
	}
	if err = m.Vault().Set(ctx, p.ID, pw, pp); err == nil {
		p, err = m.Store().Update(p.ID, sshremote.ServerPatch{ServerInput: sshremote.ServerInput{PasswordVault: promptedPassword != "", PassphraseVault: promptedPassphrase != ""}})
	}
	if err != nil {
		out["prompted_credentials_saved"] = false
		out["credential_save_error"] = err.Error()
		out["message"] = "La conexión SSH quedó abierta, pero la credencial solicitada no pudo guardarse en la bóveda cifrada."
		return jsonOutput(out)
	}
	out["prompted_credentials_saved"] = true
	out["server"] = compactProfile(p)
	return jsonOutput(out)
}

func addSSHServer(ctx context.Context, args map[string]any, env Env, m *sshremote.Manager) (string, error) {
	if hasAnyArg(args, "password", "private_key", "passphrase") {
		return "", errors.New("los perfiles persistentes no aceptan secretos literales; usa prompt_password/prompt_passphrase o referencias")
	}
	p, err := m.Store().Add(serverInput(args, env.Root))
	if err != nil {
		return "", err
	}
	rollback := true
	defer func() {
		if rollback {
			_, _ = m.Vault().DeleteServer(context.Background(), p.ID)
			_, _ = m.Store().Delete(p.ID)
		}
	}()
	if boolArgOr(args, "prompt_password", false) {
		if _, err = setVaultFieldForProfile(ctx, p, env, m, "password"); err != nil {
			return "", err
		}
		p, err = m.Store().Get(p.ID)
		if err != nil {
			return "", err
		}
	}
	if boolArgOr(args, "prompt_passphrase", false) {
		if _, err = setVaultFieldForProfile(ctx, p, env, m, "passphrase"); err != nil {
			return "", err
		}
		p, err = m.Store().Get(p.ID)
		if err != nil {
			return "", err
		}
	}
	rollback = false
	out := compactProfileWithManager(m, p)
	out["ok"] = true
	out["action"] = "add_server"
	out["message"] = "Servidor SSH guardado globalmente y disponible en todos los proyectos."
	return jsonOutput(out)
}

func updateSSHServer(ctx context.Context, args map[string]any, env Env, m *sshremote.Manager) (string, error) {
	if hasAnyArg(args, "password", "private_key", "passphrase") {
		return "", errors.New("los perfiles persistentes no aceptan secretos literales")
	}
	if !hasAnyArg(args, "name", "label", "host", "port", "username", "password_env", "private_key_path", "passphrase_env", "agent", "agent_env", "host_fingerprint_sha256", "ready_timeout_ms", "keepalive_interval_ms", "clear_name", "clear_authentication", "prompt_password", "prompt_passphrase") {
		return "", errors.New("update_server requiere al menos un campo explícito para actualizar")
	}
	ref := requiredArg(args, "server_id")
	current, err := m.Store().Get(ref)
	if err != nil {
		return "", err
	}
	if boolArgOr(args, "clear_authentication", false) && (current.PasswordVault || current.PassphraseVault) {
		if _, err = m.Vault().DeleteServer(ctx, current.ID); err != nil {
			return "", err
		}
	}
	patch := sshremote.ServerPatch{
		Fields:              serverPatchFields(args, env.Root),
		ClearName:           boolArgOr(args, "clear_name", false),
		ClearAuthentication: boolArgOr(args, "clear_authentication", false),
	}
	p, err := m.Store().Update(ref, patch)
	if err != nil {
		return "", err
	}
	if boolArgOr(args, "prompt_password", false) {
		if _, err = setVaultFieldForProfile(ctx, p, env, m, "password"); err != nil {
			return "", err
		}
		p, _ = m.Store().Get(p.ID)
	}
	if boolArgOr(args, "prompt_passphrase", false) {
		if _, err = setVaultFieldForProfile(ctx, p, env, m, "passphrase"); err != nil {
			return "", err
		}
		p, _ = m.Store().Get(p.ID)
	}
	out := compactProfileWithManager(m, p)
	out["ok"] = true
	out["action"] = "update_server"
	out["message"] = "Configuración del servidor SSH actualizada."
	return jsonOutput(out)
}

func setVaultField(ctx context.Context, args map[string]any, env Env, m *sshremote.Manager, field string) (string, error) {
	p, err := m.Store().Get(requiredArg(args, "server_id"))
	if err != nil {
		return "", err
	}
	return setVaultFieldForProfile(ctx, p, env, m, field)
}
func setVaultFieldForProfile(ctx context.Context, p sshremote.ServerProfile, env Env, m *sshremote.Manager, field string) (string, error) {
	if env.RequestSecret == nil {
		return "", errors.New("entrada secreta local no disponible")
	}
	// Open/create the vault before asking for the server credential. The user
	// therefore sees two clearly distinct prompts only when both are genuinely
	// needed, and an already-open vault is never requested a second time.
	if err := m.Vault().EnsureWritable(ctx); err != nil {
		return "", err
	}
	title := "Contraseña del servidor remoto"
	message := "Escribe la contraseña de la cuenta SSH " + sshremote.DisplayName(p) + ". Esta es la contraseña del servidor remoto, no la contraseña maestra de la bóveda. Se cifrará dentro de la bóveda local."
	secretKind := interaction.SecretRemotePassword
	if field == "passphrase" {
		title = "Passphrase de la clave privada SSH"
		message = "Escribe la passphrase que protege la clave privada de " + sshremote.DisplayName(p) + ". No es la contraseña maestra de la bóveda ni la contraseña del servidor. Se cifrará dentro de la bóveda local."
		secretKind = interaction.SecretKeyPassphrase
	}
	secret, err := env.RequestSecret(ctx, secretKind, title, message, false, 1)
	if err != nil {
		return "", err
	}
	if field == "password" {
		err = m.Vault().Set(ctx, p.ID, &secret, nil)
		if err == nil {
			p, err = m.Store().Update(p.ID, sshremote.ServerPatch{ServerInput: sshremote.ServerInput{PasswordVault: true}})
		}
	} else {
		err = m.Vault().Set(ctx, p.ID, nil, &secret)
		if err == nil {
			p, err = m.Store().Update(p.ID, sshremote.ServerPatch{ServerInput: sshremote.ServerInput{PassphraseVault: true}})
		}
	}
	secret = ""
	if err != nil {
		return "", err
	}
	out := compactProfileWithManager(m, p)
	out["ok"] = true
	out["saved"] = true
	out["field"] = field
	out["server"] = compactProfile(p)
	out["vault"], _ = m.Vault().Status()
	if field == "password" {
		out["action"] = "set_server_password"
		out["message"] = "La contraseña SSH fue guardada en la bóveda cifrada."
	} else {
		out["action"] = "set_server_passphrase"
		out["message"] = "La passphrase SSH fue guardada en la bóveda cifrada."
	}
	return jsonOutput(out)
}
func clearVaultField(ctx context.Context, args map[string]any, m *sshremote.Manager, field string) (string, error) {
	p, err := m.Store().Get(requiredArg(args, "server_id"))
	if err != nil {
		return "", err
	}
	changed, err := m.Vault().ClearField(ctx, p.ID, field)
	if err != nil {
		return "", err
	}
	patch := sshremote.ServerPatch{}
	if field == "password" {
		patch.ClearPasswordVault = true
	} else {
		patch.ClearPassphraseVault = true
	}
	p, err = m.Store().Update(p.ID, patch)
	if err != nil {
		return "", err
	}
	out := compactProfileWithManager(m, p)
	out["ok"] = true
	out["cleared"] = changed
	out["field"] = field
	out["server"] = compactProfile(p)
	if field == "password" {
		out["action"] = "clear_server_password"
		out["message"] = "La contraseña cifrada del servidor fue eliminada."
	} else {
		out["action"] = "clear_server_passphrase"
		out["message"] = "La passphrase cifrada del servidor fue eliminada."
	}
	return jsonOutput(out)
}

func connectOptions(args map[string]any, projectRoot string) sshremote.ConnectOptions {
	key := resolveProjectPath(str(args, "private_key_path"), projectRoot)
	return sshremote.ConnectOptions{Host: str(args, "host"), Port: intArgOr(args, "port", 22), Username: str(args, "username"), Password: str(args, "password"), PasswordEnv: str(args, "password_env"), PrivateKeyPath: key, PrivateKey: str(args, "private_key"), Passphrase: str(args, "passphrase"), PassphraseEnv: str(args, "passphrase_env"), Agent: str(args, "agent"), AgentEnv: str(args, "agent_env"), HostFingerprintSHA256: str(args, "host_fingerprint_sha256"), ReadyTimeoutMS: intArgOr(args, "ready_timeout_ms", 30000), KeepaliveIntervalMS: intArgOr(args, "keepalive_interval_ms", 15000), PromptPassword: boolArgOr(args, "prompt_password", false), PromptPassphrase: boolArgOr(args, "prompt_passphrase", false)}
}
func serverInput(args map[string]any, projectRoot string) sshremote.ServerInput {
	name := firstNonEmpty(str(args, "name"), str(args, "label"))
	key := resolveProjectPath(str(args, "private_key_path"), projectRoot)
	return sshremote.ServerInput{Name: name, Host: str(args, "host"), Port: intArgOr(args, "port", 0), Username: str(args, "username"), PasswordEnv: str(args, "password_env"), PrivateKeyPath: key, PassphraseEnv: str(args, "passphrase_env"), Agent: str(args, "agent"), AgentEnv: str(args, "agent_env"), HostFingerprintSHA256: str(args, "host_fingerprint_sha256"), ReadyTimeoutMS: intArgOr(args, "ready_timeout_ms", 0), KeepaliveIntervalMS: intArgOr(args, "keepalive_interval_ms", 0)}
}
func resolveProjectPath(value, projectRoot string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		return value
	}
	base := projectRoot
	if strings.TrimSpace(base) == "" {
		base = "."
	}
	if abs, err := filepath.Abs(filepath.Join(base, value)); err == nil {
		return abs
	}
	return value
}

func resolveTransferPath(value, projectRoot string) string {
	value = strings.TrimSpace(value)
	if value == "~" || strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		home, err := os.UserHomeDir()
		if err == nil {
			if value == "~" {
				return home
			}
			return filepath.Join(home, value[2:])
		}
	}
	return resolveProjectPath(value, projectRoot)
}

func compactProfiles(in []sshremote.ServerProfile) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, p := range in {
		out = append(out, compactProfile(p))
	}
	return out
}
func compactProfilesWithManager(m *sshremote.Manager, in []sshremote.ServerProfile) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, p := range in {
		out = append(out, compactProfileWithManager(m, p))
	}
	return out
}
func compactProfileWithManager(m *sshremote.Manager, p sshremote.ServerProfile) map[string]any {
	out := compactProfile(p)
	active := []map[string]any{}
	for _, c := range m.ListConnections() {
		if fmt.Sprint(c["server_id"]) == p.ID {
			active = append(active, c)
		}
	}
	out["persistent"] = true
	out["connected"] = len(active) > 0
	out["active_connection_count"] = len(active)
	out["active_connections"] = active
	return out
}
func compactProfile(p sshremote.ServerProfile) map[string]any {
	display := sshremote.DisplayName(p)
	authentication := make([]string, 0, 6)
	if p.PasswordVault {
		authentication = append(authentication, "encrypted_password_vault")
	}
	if p.PasswordEnv != "" {
		authentication = append(authentication, "password_env")
	}
	if p.PrivateKeyPath != "" {
		authentication = append(authentication, "private_key_path")
	}
	if p.PassphraseVault {
		authentication = append(authentication, "encrypted_passphrase_vault")
	}
	if p.AgentEnv != "" {
		authentication = append(authentication, "agent_env")
	}
	if p.Agent != "" {
		authentication = append(authentication, "agent")
	}
	if len(authentication) == 0 {
		authentication = []string{"not_saved"}
	}
	out := map[string]any{
		"id": p.ID, "server_id": p.ID, "ref": "ssh-server://" + p.ID, "server_ref": "ssh-server://" + p.ID,
		"name": display, "has_custom_name": p.Name != "", "display_name": display, "host": p.Host, "port": p.Port,
		"username": p.Username, "authentication": authentication, "password_vault": p.PasswordVault,
		"passphrase_vault": p.PassphraseVault, "created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
	}
	if p.Name != "" {
		out["configured_name"] = p.Name
	}
	if p.PasswordVault {
		out["password_saved"] = true
	}
	if p.PasswordEnv != "" {
		out["password_env"] = p.PasswordEnv
	}
	if p.PrivateKeyPath != "" {
		out["private_key_path"] = p.PrivateKeyPath
	}
	if p.PassphraseVault {
		out["passphrase_saved"] = true
	}
	if p.PassphraseEnv != "" {
		out["passphrase_env"] = p.PassphraseEnv
	}
	if p.AgentEnv != "" {
		out["agent_env"] = p.AgentEnv
	}
	if p.Agent != "" {
		out["agent"] = p.Agent
		out["agent_configured"] = true
	}
	if p.HostFingerprintSHA256 != "" {
		out["host_fingerprint_sha256"] = p.HostFingerprintSHA256
	}
	if p.ReadyTimeoutMS != 0 {
		out["ready_timeout_ms"] = p.ReadyTimeoutMS
	}
	if p.KeepaliveIntervalMS != 0 {
		out["keepalive_interval_ms"] = p.KeepaliveIntervalMS
	}
	return out
}
func connectionStatus(c *sshremote.Connection) map[string]any {
	s := c.Snapshot()
	out := map[string]any{
		"connection_id": s.ID, "connection_ref": "ssh://" + s.ID, "ref": "ssh://" + s.ID,
		"name": s.DisplayName, "label": s.DisplayName, "host": s.Host, "port": s.Port, "username": s.Username,
		"cwd": s.CWD, "shell_open": s.ShellOpen, "connected_at": s.CreatedAt.UTC().Format(time.RFC3339),
		"created_at": s.CreatedAt.UTC().Format(time.RFC3339), "last_used_at": s.LastUsedAt.UTC().Format(time.RFC3339),
		"connected": !s.Closed, "logical_connection_stable": !s.Closed,
		"transport_connected": s.TransportHealthy, "connection_generation": s.Generation,
		"reconnect_count": s.ReconnectCount,
	}
	if !s.LastReconnectAt.IsZero() {
		out["last_reconnect_at"] = s.LastReconnectAt.UTC().Format(time.RFC3339)
	}
	if s.LastTransportError != "" {
		out["last_transport_error"] = s.LastTransportError
	}
	if s.ServerID != "" {
		out["server_id"] = s.ServerID
		out["server_ref"] = "ssh-server://" + s.ServerID
	}
	return out
}

func addSSHPrivilegeResult(out map[string]any, privilege sshremote.PrivilegeInfo) {
	if out == nil || privilege.Method == "" {
		return
	}
	out["elevated"] = privilege.Elevated
	out["privilege_method"] = privilege.Method
}

func resolveShell(c *sshremote.Connection, ref string) (*sshremote.RemoteShell, error) {
	return c.GetShell(ref)
}
func sshPermissionForAction(action string) (config.SSHPermissionCategory, string) {
	switch action {
	case "connect", "connect_server", "close", "close_all":
		return config.SSHPermissionConnect, "administrar conexiones"
	case "list_servers", "get_server", "vault_status", "list_connections", "status", "pwd", "cd", "list", "stat", "read_file", "download", "shell_read":
		return config.SSHPermissionRead, "leer o navegar en el servidor"
	case "exec", "shell_open", "shell_write":
		return config.SSHPermissionCommands, "ejecutar comandos"
	case "upload", "write_file", "mkdir", "rename":
		return config.SSHPermissionFileChanges, "modificar archivos remotos"
	case "delete":
		return config.SSHPermissionDelete, "eliminar archivos remotos"
	case "add_server", "update_server", "rename_server", "delete_server", "change_vault_password", "set_server_password", "clear_server_password", "set_server_passphrase", "clear_server_passphrase":
		return config.SSHPermissionCredentials, "modificar perfiles o credenciales"
	case "unlock_vault", "lock_vault":
		return config.SSHPermissionVault, "cambiar el estado de la bóveda local"
	default:
		return "", ""
	}
}

func sshApprovalMessage(action, label string, args map[string]any) string {
	message := "Lilith solicita permiso para " + label + ".\nAcción: " + action
	detail := ""
	switch action {
	case "connect", "add_server":
		detail = strings.TrimSpace(str(args, "username") + "@" + str(args, "host"))
	case "connect_server", "get_server", "update_server", "rename_server", "delete_server", "set_server_password", "clear_server_password", "set_server_passphrase", "clear_server_passphrase":
		detail = strings.TrimSpace(str(args, "server_id"))
	case "exec", "shell_write":
		detail = truncateOneLineTool(str(args, "command"), 180)
	case "shell_open":
		detail = "abrir una shell interactiva"
	case "upload":
		detail = strings.TrimSpace(str(args, "local_path") + " → " + str(args, "remote_path"))
	case "download":
		detail = strings.TrimSpace(str(args, "remote_path") + " → " + str(args, "local_path"))
	case "rename":
		detail = strings.TrimSpace(str(args, "path") + " → " + str(args, "destination_path"))
	case "write_file", "mkdir", "delete", "read_file", "list", "stat", "cd":
		detail = strings.TrimSpace(str(args, "path"))
	case "status", "pwd", "close":
		detail = strings.TrimSpace(str(args, "connection_id"))
	case "shell_read":
		detail = strings.TrimSpace(str(args, "shell_id"))
	case "unlock_vault":
		detail = "desbloquear la bóveda para esta ejecución"
	case "lock_vault":
		detail = "eliminar la clave maestra de la memoria"
	case "change_vault_password":
		detail = "volver a cifrar la bóveda con una contraseña nueva"
	}
	if detail != "" {
		message += "\nDestino: " + detail
	}
	if reason := strings.TrimSpace(str(args, "reason")); reason != "" {
		message += "\nMotivo: " + truncateOneLineTool(reason, 240)
	}
	return message
}

func sshApprovalRule(action string, args map[string]any, manager *sshremote.Manager) string {
	target := strings.TrimSpace(str(args, "server_id"))
	if target == "" {
		if connectionID := strings.TrimSpace(str(args, "connection_id")); connectionID != "" && manager != nil {
			if conn, err := manager.Get(connectionID); err == nil {
				snapshot := conn.Snapshot()
				if snapshot.ServerID != "" {
					target = snapshot.ServerID
				} else {
					target = fmt.Sprintf("%s@%s:%d", snapshot.Username, snapshot.Host, snapshot.Port)
				}
			}
			if target == "" {
				target = connectionID
			}
		}
	}
	if target == "" {
		host := strings.TrimSpace(str(args, "host"))
		if host != "" {
			target = fmt.Sprintf("%s@%s:%d", strings.TrimSpace(str(args, "username")), host, intArgOr(args, "port", 22))
		}
	}
	if target == "" {
		target = "global"
	}
	return strings.ToLower(strings.TrimSpace(action)) + "|" + strings.ToLower(target)
}

func truncateOneLineTool(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if max <= 0 || len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

func authorizeProtectedEnv(ctx context.Context, env Env, title, p string) error {
	if env.Confirm == nil {
		return errors.New("la protección .env requiere confirmación local")
	}
	ok, err := env.Confirm(ctx, title, "La operación solicita acceso al archivo protegido "+p+". Autoriza sólo si comprendes que puede contener credenciales.")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("acceso a .env cancelado por el usuario")
	}
	return nil
}
func isProtectedEnv(p string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(p)))
	if base == ".env" {
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		for _, x := range []string{"example", "sample", "template", "dist", "defaults"} {
			if base == ".env."+x {
				return false
			}
		}
		return true
	}
	return false
}
func withAction(action string, value any) map[string]any {
	data, _ := json.Marshal(value)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	out["action"] = action
	return out
}

func waitSSH(ctx context.Context, milliseconds int) error {
	if milliseconds <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(milliseconds) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func shellToolResult(action string, conn *sshremote.Connection, result map[string]any) map[string]any {
	result["action"] = action
	result["connection_id"] = conn.ID
	result["cwd"] = conn.Snapshot().CWD
	if id := fmt.Sprint(result["shell_id"]); id != "" {
		result["shell_ref"] = "shell://" + id
	}
	return result
}

func trimSSHOutput(value string, max int) (string, bool) {
	if max <= 0 {
		max = 200000
	}
	data := []byte(value)
	if len(data) <= max {
		return value, false
	}
	data = data[:max]
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	return string(data), true
}

func encodedLength(content, encoding string) (int, error) {
	if encoding != "base64" {
		return len([]byte(content)), nil
	}
	data, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func fileSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

func jsonOutput(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
func requiredArg(args map[string]any, key string) string {
	v := strings.TrimSpace(str(args, key))
	if v == "" {
		return ""
	}
	return v
}
func hasAnyArg(args map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := args[key]; ok {
			return true
		}
	}
	return false
}

func serverPatchFields(args map[string]any, projectRoot string) map[string]any {
	fields := map[string]any{}
	aliases := map[string]string{"label": "name"}
	for _, key := range []string{"name", "label", "host", "port", "username", "password_env", "private_key_path", "passphrase_env", "agent", "agent_env", "host_fingerprint_sha256", "ready_timeout_ms", "keepalive_interval_ms"} {
		value, ok := args[key]
		if !ok {
			continue
		}
		target := key
		if alias := aliases[key]; alias != "" {
			target = alias
		}
		if target == "private_key_path" {
			if text, ok := value.(string); ok && text != "" && !filepath.IsAbs(text) {
				base := projectRoot
				if strings.TrimSpace(base) == "" {
					base = "."
				}
				if abs, err := filepath.Abs(filepath.Join(base, text)); err == nil {
					value = abs
				}
			}
		}
		fields[target] = value
	}
	return fields
}

func secondsArg(args map[string]any, key string, def int) time.Duration {
	n := intArgOr(args, key, def)
	if n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
func defaultString(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
