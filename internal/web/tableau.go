package web

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"parallax/internal/xlsx"
)

// Export universel (backlog v3.0) : tout tableau affiché dans Parallax
// s'exporte en CSV et en xlsx, par une seule convention.
//
// Côté handler, un écran de liste commence par :
//
//	if s.exporter(w, r, func() (tableau, error) { ... }) { return }
//
// La fermeture construit le tableau à partir des mêmes données et des mêmes
// filtres que la page ; elle n'est appelée que si la requête porte
// ?export=csv ou ?export=xlsx. Le handler continue sinon vers le rendu
// HTML, en passant liensExport(r) au gabarit, qui pose les deux boutons via
// {{template "export_boutons" .Export}}.
//
// Les cellules acceptent string, float64, int, int64, bool et nil ; les
// nombres restent des nombres dans le xlsx et sont formatés sans bruit de
// virgule flottante dans le CSV.

// tableau est la forme exportable d'une liste : un titre (nom de fichier et
// de feuille), des colonnes et des lignes.
type tableau struct {
	Titre    string
	Colonnes []string
	Lignes   [][]any
}

// exportLiens porte les deux URL d'export d'un écran, construites à partir
// de la requête courante — filtres, scénario et tri compris.
type exportLiens struct {
	CSV  string
	XLSX string
}

// liensExport dérive les URL d'export de la requête courante, en conservant
// tous ses paramètres.
func liensExport(r *http.Request) exportLiens {
	return exportLiens{CSV: lienExport(r, "csv"), XLSX: lienExport(r, "xlsx")}
}

func lienExport(r *http.Request, format string) string {
	q := r.URL.Query()
	q.Set("export", format)
	return r.URL.Path + "?" + q.Encode()
}

// Plusieurs tableaux sur une même page (détail d'un modèle : composants et
// nœuds de chaque révision ; détail d'un serveur : rattachements et
// affectations ; besoin/offre et sa section contraintes…) : chaque tableau
// porte un nom, ajouté à l'URL sous ?tableau=<nom>. Le handler enchaîne
// alors autant d'appels s.exporterNomme(w, r, nom, …) que de tableaux, et
// les boutons sont posés avec liensExportPour(r, nom).
//
// Fragments htmx : quand un tableau est rechargé par un fragment (filtres,
// case « inclure les archivés », création), les boutons vivent dans le
// fragment et pointent vers l'URL de la PAGE, pas du fragment, en reprenant
// les paramètres courants — liensExportVers(r, cheminPage, nom, params…).
// C'est le handler de la page qui répond à l'export : la page et le fragment
// lisent les mêmes filtres depuis la requête, donc le fichier exporté
// correspond toujours au tableau affiché.

// liensExportPour dérive les URL d'export d'un tableau nommé de la requête
// courante, en conservant tous ses paramètres.
func liensExportPour(r *http.Request, nom string) exportLiens {
	return liensExportDepuis(r.URL.Path, r.URL.Query(), nom)
}

// liensExportVers construit les liens d'export vers cheminPage (l'URL de la
// page dont le fragment courant fait partie) en ne reprenant, parmi les
// paramètres de la requête, que ceux nommés — lus en GET (chaîne de requête,
// rechargement htmx d'un tableau filtré) comme en POST (corps, création qui
// réaffiche le tableau avec ses filtres). Les champs du formulaire de
// création lui-même (code, libellé…) ne sont ainsi jamais recopiés.
func liensExportVers(r *http.Request, cheminPage, nom string, parametres ...string) exportLiens {
	_ = r.ParseForm() // sans effet sur un GET déjà parsé ; obligatoire pour lire le corps d'un POST
	q := url.Values{}
	for _, p := range parametres {
		for _, v := range r.Form[p] {
			if v != "" {
				q.Add(p, v)
			}
		}
	}
	return liensExportDepuis(cheminPage, q, nom)
}

// liensExportDepuis est la forme explicite : un chemin, des paramètres et un
// nom de tableau (vide pour l'unique tableau d'une page). Sert aux fragments
// construits sans requête sous la main (blocs d'une révision, sections d'un
// serveur), qui connaissent leurs identifiants mais pas r.
func liensExportDepuis(chemin string, params url.Values, nom string) exportLiens {
	q := url.Values{}
	for k, vs := range params {
		if k == "export" || k == "tableau" {
			continue
		}
		q[k] = append([]string(nil), vs...)
	}
	if nom != "" {
		q.Set("tableau", nom)
	}
	lien := func(format string) string {
		q.Set("export", format)
		return chemin + "?" + q.Encode()
	}
	return exportLiens{CSV: lien("csv"), XLSX: lien("xlsx")}
}

// exporterNomme est exporter pour un tableau nommé : ne répond que si
// ?export est présent ET que ?tableau désigne ce nom (ou est absent quand
// nom est vide). Une page à plusieurs tableaux l'appelle une fois par
// tableau ; le premier qui correspond répond et le handler s'arrête.
func (s *serveur) exporterNomme(w http.ResponseWriter, r *http.Request, nom string, construire func() (tableau, error)) bool {
	q := r.URL.Query()
	if q.Get("export") == "" || q.Get("tableau") != nom {
		return false
	}
	return s.exporter(w, r, construire)
}

// exporter répond au paramètre ?export=csv|xlsx s'il est présent et renvoie
// vrai — le handler doit alors s'arrêter. Sans paramètre (ou avec une valeur
// inconnue), rien n'est écrit et le retour est faux.
func (s *serveur) exporter(w http.ResponseWriter, r *http.Request, construire func() (tableau, error)) bool {
	format := r.URL.Query().Get("export")
	if format != "csv" && format != "xlsx" {
		return false
	}
	t, err := construire()
	if err != nil {
		s.erreurServeur(w, r, err)
		return true
	}
	switch format {
	case "csv":
		ecrireTableauCSV(w, t)
	case "xlsx":
		if err := ecrireTableauXLSX(w, t); err != nil {
			// des octets sont peut-être déjà partis (zip en flux) : on ne
			// peut plus changer le code de statut, seulement journaliser.
			s.erreurServeur(w, r, err)
		}
	}
	return true
}

// nomFichierExport dérive un nom de fichier sûr du titre : minuscules,
// accents et ponctuation ramenés à des tirets, date du jour en suffixe.
func nomFichierExport(titre, extension string) string {
	var b strings.Builder
	dernierTiret := true
	for _, c := range strings.ToLower(titre) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteRune(c)
			dernierTiret = false
		case c == 'é', c == 'è', c == 'ê', c == 'ë':
			b.WriteRune('e')
			dernierTiret = false
		case c == 'à', c == 'â':
			b.WriteRune('a')
			dernierTiret = false
		case c == 'ç':
			b.WriteRune('c')
			dernierTiret = false
		case c == 'î', c == 'ï':
			b.WriteRune('i')
			dernierTiret = false
		case c == 'ô':
			b.WriteRune('o')
			dernierTiret = false
		case c == 'ù', c == 'û':
			b.WriteRune('u')
			dernierTiret = false
		default:
			if !dernierTiret {
				b.WriteRune('-')
				dernierTiret = true
			}
		}
	}
	nom := strings.Trim(b.String(), "-")
	if nom == "" {
		nom = "export"
	}
	return fmt.Sprintf("%s-%s.%s", nom, time.Now().Format("2006-01-02"), extension)
}

func celluleTexte(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return formaterNombre(x)
	case bool:
		return ouiNon(x)
	default:
		return fmt.Sprint(x)
	}
}

func ecrireTableauCSV(w http.ResponseWriter, t tableau) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+url.PathEscape(nomFichierExport(t.Titre, "csv"))+`"`)

	ew := csv.NewWriter(w)
	ew.Comma = ';' // Excel FR ouvre un CSV point-virgule sans ré-import manuel
	_ = ew.Write(t.Colonnes)
	for _, l := range t.Lignes {
		ligne := make([]string, len(l))
		for i, v := range l {
			ligne[i] = celluleTexte(v)
		}
		_ = ew.Write(ligne)
	}
	ew.Flush()
}

func ecrireTableauXLSX(w http.ResponseWriter, t tableau) error {
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+url.PathEscape(nomFichierExport(t.Titre, "xlsx"))+`"`)
	nom := t.Titre
	if nom == "" {
		nom = "Export"
	}
	return xlsx.Ecrire(w, xlsx.Feuille{Nom: nom, Entetes: t.Colonnes, Lignes: t.Lignes})
}

// texteOuVide rend un *string exportable : nil devient une cellule vide.
func texteOuVide(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// nombreOuVide rend un *float64 exportable.
func nombreOuVide(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// entierOuVide rend un *int64 exportable.
func entierOuVide(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}
