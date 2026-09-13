package i18n

// Clés "contrainte.aide.<code>" et "licence.aide.{mecanisme,niveau}.<code>" :
// l'aide d'une ligne associée à chaque option d'un <select>, construite dans
// le gabarit via {{t (printf "contrainte.aide.%s" .Code)}} (voir
// templates/clusters/contraintes.html et templates/licences/liste.html) —
// les codes eux-mêmes viennent de internal/capacity et internal/depot.
func init() {
	Ajouter(map[string]string{
		"contrainte.aide.MIN_TOTAL":         "Nombre minimum de serveurs sur l'ensemble du cluster.",
		"contrainte.aide.MIN_PAR_ZONE":      "Nombre minimum de serveurs par zone.",
		"contrainte.aide.MULTIPLE_TOTAL":    "Arrondit le nombre total de serveurs au multiple supérieur de cette valeur.",
		"contrainte.aide.MULTIPLE_PAR_ZONE": "Arrondit le nombre de serveurs par zone au multiple supérieur de cette valeur.",
		"contrainte.aide.NB_ZONES":          "Nombre de zones sur lesquels répartir le dimensionnement.",
		"contrainte.aide.EQUILIBRAGE_ZONE":  "Répartit les serveurs à parts égales entre les zones — sans valeur numérique, mais avec une portée (nouveaux serveurs seulement, ou ensemble du parc).",

		"licence.aide.mecanisme.NOEUDS":         "Somme des nœuds (le niveau est sans objet).",
		"licence.aide.mecanisme.MAX_NOEUDS_RAM": "max(nœuds, ceil(RAM / RAM max)) au niveau choisi.",
		"licence.aide.mecanisme.RAM":            "ceil(RAM / RAM max) au niveau choisi.",

		"licence.aide.niveau.MACHINE": "Par serveur, puis somme.",
		"licence.aide.niveau.CLUSTER": "Par cluster, puis somme.",
		"licence.aide.niveau.GLOBAL":  "Sur tout le groupe de la vue — non additif entre sous-totaux.",
	}, map[string]string{
		"contrainte.aide.MIN_TOTAL":         "Minimum number of servers across the whole cluster.",
		"contrainte.aide.MIN_PAR_ZONE":      "Minimum number of servers per zone.",
		"contrainte.aide.MULTIPLE_TOTAL":    "Rounds the total number of servers up to the next multiple of this value.",
		"contrainte.aide.MULTIPLE_PAR_ZONE": "Rounds the number of servers per zone up to the next multiple of this value.",
		"contrainte.aide.NB_ZONES":          "Number of zones to spread the sizing across.",
		"contrainte.aide.EQUILIBRAGE_ZONE":  "Spreads servers evenly across zones — no numeric value, but a scope (new servers only, or the whole fleet).",

		"licence.aide.mecanisme.NOEUDS":         "Sum of nodes (the level does not apply).",
		"licence.aide.mecanisme.MAX_NOEUDS_RAM": "max(nodes, ceil(RAM / max RAM)) at the chosen level.",
		"licence.aide.mecanisme.RAM":            "ceil(RAM / max RAM) at the chosen level.",

		"licence.aide.niveau.MACHINE": "Per server, then summed.",
		"licence.aide.niveau.CLUSTER": "Per cluster, then summed.",
		"licence.aide.niveau.GLOBAL":  "Across the whole view group — not additive between subtotals.",
	})
}
