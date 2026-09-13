package i18n

// Clés du fragment "export_boutons" (internal/web/templates/export.html),
// partagé par tous les écrans exportables — voir tableau.go.
func init() {
	Ajouter(map[string]string{
		"export.csv":   "Exporter ce tableau en CSV",
		"export.excel": "Exporter ce tableau pour Excel",
	}, map[string]string{
		"export.csv":   "Export this table as CSV",
		"export.excel": "Export this table for Excel",
	})
}
