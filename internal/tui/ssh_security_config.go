package tui

import (
	"fmt"
	"strings"

	"github.com/lilith/li/internal/config"
	"github.com/lilith/li/internal/tui/uikit"
	tuistyle "github.com/lilith/li/internal/tui/uikit/style"
)

var sshApprovalModes = []config.SSHApprovalMode{
	config.SSHApprovalCriticalOnly,
	config.SSHApprovalEveryAction,
	config.SSHApprovalCommandsOnly,
	config.SSHApprovalTrustModel,
	config.SSHApprovalCustom,
}

var sshCustomCategories = []struct {
	ID          string
	Category    config.SSHPermissionCategory
	Title       string
	Description string
}{
	{"ssh-custom-connect", config.SSHPermissionConnect, "Conexiones", "Pedir aprobación antes de abrir una conexión SSH nueva."},
	{"ssh-custom-read", config.SSHPermissionRead, "Lecturas y navegación", "Proteger listados, lectura/descarga de archivos y cambios de directorio remoto."},
	{"ssh-custom-commands", config.SSHPermissionCommands, "Comandos y shells", "Pedir aprobación antes de exec, abrir una shell o escribir comandos en ella."},
	{"ssh-custom-files", config.SSHPermissionFileChanges, "Cambios de archivos", "Proteger subidas, escrituras, creación de carpetas y renombrados."},
	{"ssh-custom-delete", config.SSHPermissionDelete, "Eliminaciones", "Pedir aprobación antes de borrar archivos o directorios remotos."},
	{"ssh-custom-credentials", config.SSHPermissionCredentials, "Perfiles y credenciales", "Proteger cambios en servidores guardados, contraseñas, passphrases y bóveda."},
	{"ssh-custom-vault", config.SSHPermissionVault, "Bloqueo de la bóveda", "Pedir aprobación adicional al bloquear o desbloquear manualmente la bóveda local."},
}

func sshApprovalModeLabel(mode config.SSHApprovalMode) string {
	switch mode {
	case config.SSHApprovalEveryAction:
		return "Cada acción"
	case config.SSHApprovalCommandsOnly:
		return "Sólo comandos"
	case config.SSHApprovalTrustModel:
		return "Confiar en el modelo"
	case config.SSHApprovalCustom:
		return "Personalizado"
	default:
		return "Cambios críticos"
	}
}

func sshApprovalModeDescription(mode config.SSHApprovalMode) string {
	switch mode {
	case config.SSHApprovalEveryAction:
		return "Pide aprobación para conexiones, lecturas, comandos, cambios, eliminaciones y credenciales."
	case config.SSHApprovalCommandsOnly:
		return "Sólo interrumpe al ejecutar comandos o usar una shell remota."
	case config.SSHApprovalTrustModel:
		return "No solicita aprobaciones SSH. Las protecciones de .env y la contraseña de la bóveda siguen separadas."
	case config.SSHApprovalCustom:
		return "Usa exactamente las categorías activadas debajo."
	default:
		return "No interrumpe comandos normales; pide aprobación para cambios de archivos, eliminaciones y credenciales."
	}
}

func (c *ConfigScreen) openSSHSecurity() (uikit.Model, uikit.Cmd) {
	c.sshSecurityOpen = true
	mode := config.NormalizeSSHRemoteSecurity(c.settings.SSHRemote).Mode
	c.focus = "ssh-mode:" + string(mode)
	c.viewportOffset = 0
	return c, nil
}

func (c *ConfigScreen) closeSSHSecurity() (uikit.Model, uikit.Cmd) {
	c.sshSecurityOpen = false
	c.focus = "ssh-remote"
	c.viewportOffset = 0
	return c, nil
}

func (c *ConfigScreen) sshSecurityFocusOrder() []string {
	order := make([]string, 0, len(sshApprovalModes)+len(sshCustomCategories)+2)
	for _, mode := range sshApprovalModes {
		order = append(order, "ssh-mode:"+string(mode))
	}
	for _, item := range sshCustomCategories {
		order = append(order, item.ID)
	}
	return append(order, "ssh-project-approvals-clear", "ssh-security-back")
}

func (c *ConfigScreen) updateSSHSecurityKey(v uikit.KeyMsg) (uikit.Model, uikit.Cmd) {
	switch v.String() {
	case "esc", "q":
		return c.closeSSHSecurity()
	case "down", "j", "tab":
		c.moveSSHSecurityFocus(1)
		return c, nil
	case "up", "k", "shift+tab":
		c.moveSSHSecurityFocus(-1)
		return c, nil
	case "enter", " ":
		return c.handleSSHSecurityAction(c.focus)
	}
	return c, nil
}

func (c *ConfigScreen) moveSSHSecurityFocus(delta int) {
	order := c.sshSecurityFocusOrder()
	idx := 0
	for i, id := range order {
		if id == c.focus {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(order) {
		idx = len(order) - 1
	}
	c.focus = order[idx]
}

func (c *ConfigScreen) handleSSHSecurityAction(id string) (uikit.Model, uikit.Cmd) {
	if id == "ssh-security-back" {
		return c.closeSSHSecurity()
	}
	if id == "ssh-project-approvals-clear" {
		removed := config.ClearSSHProjectApprovals(&c.settings, currentProject())
		if removed == 0 {
			c.message = "Este proyecto no tiene permisos SSH permanentes guardados."
			return c, nil
		}
		return c.saveSSHSecurity(fmt.Sprintf("Se eliminaron %d permisos SSH permanentes de este proyecto.", removed))
	}
	if strings.HasPrefix(id, "ssh-mode:") {
		mode := config.SSHApprovalMode(strings.TrimPrefix(id, "ssh-mode:"))
		for _, allowed := range sshApprovalModes {
			if mode == allowed {
				c.settings.SSHRemote.Mode = mode
				return c.saveSSHSecurity("Política SSH actualizada a " + sshApprovalModeLabel(mode) + ".")
			}
		}
		return c, nil
	}
	for _, item := range sshCustomCategories {
		if item.ID != id {
			continue
		}
		enabled := !sshCustomPermissionEnabled(c.settings.SSHRemote, item.Category)
		config.SetSSHCustomPermission(&c.settings.SSHRemote, item.Category, enabled)
		state := "desactivada"
		if enabled {
			state = "activada"
		}
		return c.saveSSHSecurity(fmt.Sprintf("Categoría %s %s; política Personalizado activa.", item.Title, state))
	}
	return c, nil
}

func (c *ConfigScreen) saveSSHSecurity(message string) (uikit.Model, uikit.Cmd) {
	if err := config.Save(c.ctx.ConfigDir, c.settings); err != nil {
		c.danger = "No se pudo guardar: " + err.Error()
		return c, nil
	}
	c.danger = ""
	c.message = message
	return c, nil
}

func sshCustomPermissionEnabled(security config.SSHRemoteSecurity, category config.SSHPermissionCategory) bool {
	switch category {
	case config.SSHPermissionConnect:
		return security.ConfirmConnect
	case config.SSHPermissionRead:
		return security.ConfirmRead
	case config.SSHPermissionCommands:
		return security.ConfirmCommands
	case config.SSHPermissionFileChanges:
		return security.ConfirmFileChanges
	case config.SSHPermissionDelete:
		return security.ConfirmDelete
	case config.SSHPermissionCredentials:
		return security.ConfirmCredentials
	case config.SSHPermissionVault:
		return security.ConfirmVault
	default:
		return false
	}
}

func (c *ConfigScreen) appendSSHSecurityLayout(canvas *settingsCanvas, width int) {
	s := c.ctx.Styles
	canvas.block(settingsCard(s, settingsCardSpec{
		Title:       "Política activa",
		Description: sshApprovalModeDescription(c.settings.SSHRemote.Mode),
		Badge:       strings.ToUpper(sshApprovalModeLabel(c.settings.SSHRemote.Mode)),
		Width:       width,
	}))
	canvas.blank()
	canvas.block(settingsButtonGroup(s, width,
		settingsButtonSpec{ID: "ssh-mode:" + string(config.SSHApprovalCriticalOnly), Label: "Cambios críticos", Active: c.settings.SSHRemote.Mode == config.SSHApprovalCriticalOnly, Focused: c.focus == "ssh-mode:"+string(config.SSHApprovalCriticalOnly)},
		settingsButtonSpec{ID: "ssh-mode:" + string(config.SSHApprovalEveryAction), Label: "Cada acción", Active: c.settings.SSHRemote.Mode == config.SSHApprovalEveryAction, Focused: c.focus == "ssh-mode:"+string(config.SSHApprovalEveryAction)},
		settingsButtonSpec{ID: "ssh-mode:" + string(config.SSHApprovalCommandsOnly), Label: "Sólo comandos", Active: c.settings.SSHRemote.Mode == config.SSHApprovalCommandsOnly, Focused: c.focus == "ssh-mode:"+string(config.SSHApprovalCommandsOnly)},
		settingsButtonSpec{ID: "ssh-mode:" + string(config.SSHApprovalTrustModel), Label: "Confiar en el modelo", Active: c.settings.SSHRemote.Mode == config.SSHApprovalTrustModel, Focused: c.focus == "ssh-mode:"+string(config.SSHApprovalTrustModel)},
		settingsButtonSpec{ID: "ssh-mode:" + string(config.SSHApprovalCustom), Label: "Personalizado", Active: c.settings.SSHRemote.Mode == config.SSHApprovalCustom, Focused: c.focus == "ssh-mode:"+string(config.SSHApprovalCustom)},
	))
	canvas.blank()
	canvas.block(settingsHeader(s, "Permisos personalizados", "Esta matriz queda guardada. Cambiar una categoría activa automáticamente la política Personalizado."))
	for _, item := range sshCustomCategories {
		canvas.blank()
		canvas.block(c.toggleFocusCard(width, item.ID, item.Title, item.Description, sshCustomPermissionEnabled(c.settings.SSHRemote, item.Category)))
	}
	canvas.blank()
	remembered := config.CountSSHProjectApprovals(c.settings, currentProject())
	canvas.block(c.navigationFocusCard(
		width,
		"ssh-project-approvals-clear",
		"Permisos permanentes de este proyecto",
		"Elimina las decisiones «Permitir siempre en este proyecto» sin cambiar la política SSH general.",
		fmt.Sprintf("%d guardados", remembered),
	))
	canvas.blank()
	canvas.block(c.navigationFocusCard(width, "ssh-security-back", "Volver a Seguridad", "Regresa a los controles generales de seguridad.", ""))
}

func (c *ConfigScreen) navigationFocusCard(width int, id, title, description, badge string) settingsBlock {
	s := c.ctx.Styles
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	marker := "  "
	if c.focus == id {
		marker = "› "
	}
	titleText := marker + s.Title.Render(title)
	status := ""
	if strings.TrimSpace(badge) != "" {
		status = s.Accent.Render("[" + badge + "]")
	}
	line := titleText
	if status != "" {
		gap := inner - tuistyle.Width(titleText) - tuistyle.Width(status)
		if gap < 2 {
			gap = 2
		}
		line += strings.Repeat(" ", gap) + status
	}
	lines := []string{line, s.Muted.Render(settingsWrapPlain(description, inner))}
	style := tuistyle.NewStyle().Width(inner).Padding(0, 1).Border(tuistyle.RoundedBorder()).BorderForeground(s.Theme.Border)
	if c.focus == id {
		style = style.BorderForeground(s.Theme.BorderFocus).Background(s.Theme.SurfaceHover).Bold(true)
	}
	text := style.Render(strings.Join(lines, "\n"))
	return settingsBlock{text: text, hits: []settingsHit{{id: id, rect: settingsRect{w: tuistyle.Width(text), h: tuistyle.Height(text)}}}}
}
