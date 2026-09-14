package web

// fusionsVerticales calcule, pour un tableau dont chaque ligne porte les
// valeurs de ses axes (déjà triées, donc adjacentes quand égales), la
// fusion verticale des cellules d'axes : la vue hiérarchique attendue
// quand plusieurs axes sont choisis. Une cellule fusionne avec celle du
// dessus si toutes les valeurs des axes précédents et du sien sont égales
// — jamais sur la seule égalité de sa colonne, sinon deux « Hot » sous
// deux technos différentes se souderaient.
//
// Renvoie, ligne par ligne et axe par axe, le rowspan à poser : n pour la
// première cellule d'une série de n, 0 pour une cellule à ne pas rendre.
// Les exports restent plats (valeurs répétées) : la fusion est un rendu.
func fusionsVerticales(cles [][]string) [][]int {
	spans := make([][]int, len(cles))
	for i := range cles {
		spans[i] = make([]int, len(cles[i]))
	}
	if len(cles) == 0 {
		return spans
	}
	for j := range cles[0] {
		debut := 0
		for i := 1; i <= len(cles); i++ {
			if i == len(cles) || !memePrefixe(cles[i], cles[debut], j) {
				spans[debut][j] = i - debut
				debut = i
			}
		}
	}
	return spans
}

func memePrefixe(a, b []string, jusqua int) bool {
	if jusqua >= len(a) || jusqua >= len(b) {
		return false
	}
	for k := 0; k <= jusqua; k++ {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}
