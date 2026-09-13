package web

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"parallax/internal/depot"
	"parallax/internal/importation"
)

// routesImportReferentiels enregistre l'import des six référentiels (projet,
// environnement, techno, tier, usage, zone) en un seul fichier : une colonne
// « type » distingue les lignes. Ce sont peu de lignes, mais les ressaisir à
// la main sur chaque poste de test ou à chaque remise à zéro de base est
// une corvée inutile — et un fichier versionné vaut mieux qu'une saisie.
//
// Même discipline que les autres imports (docs/backlog.md v1.4) : analyse
// sans écriture, confirmation dans une transaction unique, jamais d'import
// partiel. Un code déjà présent en base est une erreur de ligne, pas une
// mise à jour silencieuse : l'import sert à la reprise, pas à la
// synchronisation.
func (s *serveur) routesImportReferentiels() {
	s.mux.HandleFunc("GET /import/referentiels", s.lecteur(s.importReferentielsPage))
	s.mux.HandleFunc("POST /import/referentiels/analyser", s.editeur(s.importReferentielsAnalyser))
	s.mux.HandleFunc("POST /import/referentiels/confirmer", s.editeur(s.importReferentielsConfirmer))
}

func (s *serveur) importReferentielsPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_referentiels"), "import_referentiels_page", nil)
}

// ---------------------------------------------------------------- format

// typeReferentielImport décrit un type de ligne : la table cible, la
// méthode de lecture par code (pour refuser les doublons avec la base) et
// les colonnes facultatives qu'il comprend.
type typeReferentielImport struct {
	Table    string
	Libelle  string
	AOrdre   bool // ENVIRONNEMENT, TIER
	ASite    bool // ZONE
	Existant func(d *depot.Depot, code string) error
}

var typesReferentielsImport = map[string]typeReferentielImport{
	"PROJET": {Table: "projet", Libelle: "projet",
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireProjetParCode(code); return err }},
	"ENVIRONNEMENT": {Table: "environnement", Libelle: "environnement", AOrdre: true,
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireEnvironnementParCode(code); return err }},
	"TECHNO": {Table: "techno", Libelle: "techno",
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireTechnoParCode(code); return err }},
	"TIER": {Table: "tier", Libelle: "tier", AOrdre: true,
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireTierParCode(code); return err }},
	"USAGE": {Table: "usage_fonctionnel", Libelle: "usage",
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireUsageParCode(code); return err }},
	"ZONE": {Table: "zone", Libelle: "zone",
		ASite:    true,
		Existant: func(d *depot.Depot, code string) error { _, err := d.LireZoneParCode(code); return err }},
}

// ordreTypesReferentiels fixe l'ordre des types dans les résumés et
// l'écriture — stable, lisible, indépendant de l'ordre du fichier.
var ordreTypesReferentiels = []string{"PROJET", "ENVIRONNEMENT", "TECHNO", "TIER", "USAGE", "ZONE"}

var entetesObligatoiresImportReferentiels = []string{"type", "code", "libelle"}

// ligneReferentielImport est une ligne validée, prête à écrire.
type ligneReferentielImport struct {
	NumeroLigne int
	Type        string
	Code        string
	Libelle     string
	Ordre       int64
	Site        *string
}

// ---------------------------------------------------------------- validation

// validerLignesReferentiels remonte toutes les anomalies du fichier
// ensemble. Le second retour n'a de sens que sans erreur. Le troisième est
// une panne (base indisponible), jamais une anomalie de saisie.
func validerLignesReferentiels(d *depot.Depot, entetes []string, lignes []map[string]string, numeros []int) (importation.Rapport, []ligneReferentielImport, error) {
	manquantes := importation.EntetesManquantes(entetes, entetesObligatoiresImportReferentiels)
	if len(manquantes) > 0 {
		return importation.Rapport{
			Erreurs: []importation.ErreurLigne{{
				Ligne:   1,
				Message: fmt.Sprintf("colonne(s) obligatoire(s) manquante(s) : %s", strings.Join(manquantes, ", ")),
			}},
		}, nil, nil
	}

	// doublons intra-fichier sur (type, code)
	lignesParCle := map[string][]int{}
	for i, ligne := range lignes {
		cle := strings.ToUpper(strings.TrimSpace(ligne["type"])) + "\x1f" + strings.TrimSpace(ligne["code"])
		lignesParCle[cle] = append(lignesParCle[cle], numeros[i])
	}

	var erreurs []importation.ErreurLigne
	var valides []ligneReferentielImport

	for i, ligne := range lignes {
		numero := numeros[i]
		var messages []string

		typeVal := strings.ToUpper(strings.TrimSpace(ligne["type"]))
		spec, typeConnu := typesReferentielsImport[typeVal]
		switch {
		case typeVal == "":
			messages = append(messages, "type vide")
		case !typeConnu:
			messages = append(messages, fmt.Sprintf("type « %s » inconnu (attendu : %s)", typeVal, strings.Join(ordreTypesReferentiels, ", ")))
		}

		code := strings.TrimSpace(ligne["code"])
		libelle := strings.TrimSpace(ligne["libelle"])
		if code == "" {
			messages = append(messages, "code vide")
		}
		if libelle == "" {
			messages = append(messages, "libelle vide")
		}
		if typeConnu && code != "" {
			cle := typeVal + "\x1f" + code
			if len(lignesParCle[cle]) > 1 {
				messages = append(messages, fmt.Sprintf("%s « %s » dupliqué dans le fichier (lignes %s)", spec.Libelle, code, joindreNumeros(lignesParCle[cle])))
			} else {
				err := spec.Existant(d, code)
				switch {
				case err == nil:
					messages = append(messages, fmt.Sprintf("%s « %s » existe déjà", spec.Libelle, code))
				case errors.Is(err, depot.ErrIntrouvable):
					// libre, c'est le cas attendu
				default:
					return importation.Rapport{}, nil, err
				}
			}
		}

		var ordre int64
		if typeConnu && spec.AOrdre {
			v, err := importation.ParserEntier(ligne["ordre"])
			if err != nil {
				messages = append(messages, "ordre : "+err.Error())
			} else if v != nil {
				ordre = *v
			}
		}
		var site *string
		if typeConnu && spec.ASite {
			site = texteOptionnelCSV(ligne["site"])
		}

		if len(messages) > 0 {
			erreurs = append(erreurs, importation.ErreurLigne{Ligne: numero, Message: strings.Join(messages, " ; ")})
			continue
		}
		valides = append(valides, ligneReferentielImport{
			NumeroLigne: numero, Type: typeVal, Code: code, Libelle: libelle, Ordre: ordre, Site: site,
		})
	}

	return importation.Rapport{NbLignes: len(lignes), Erreurs: erreurs}, valides, nil
}

// ---------------------------------------------------------------- écriture

// executerImportReferentiels écrit toutes les lignes dans une transaction
// unique sur Depot.Base() — même justification que executerImportModeles :
// les méthodes CreerXxx du dépôt ouvrent chacune leur transaction. Renvoie
// le nombre de lignes créées par type, dans l'ordre d'ordreTypesReferentiels.
func executerImportReferentiels(d *depot.Depot, valides []ligneReferentielImport) (map[string]int, error) {
	tx, err := d.Base().Begin()
	if err != nil {
		return nil, fmt.Errorf("ouverture de la transaction d'import des référentiels : %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	comptes := map[string]int{}
	for _, l := range valides {
		spec := typesReferentielsImport[l.Type]
		var res sql.Result
		var errExec error
		switch {
		case spec.AOrdre:
			res, errExec = tx.Exec(
				fmt.Sprintf(`INSERT INTO %s (code, libelle, ordre) VALUES (?, ?, ?)`, spec.Table),
				l.Code, l.Libelle, l.Ordre)
		case spec.ASite:
			res, errExec = tx.Exec(
				fmt.Sprintf(`INSERT INTO %s (code, libelle, site) VALUES (?, ?, ?)`, spec.Table),
				l.Code, l.Libelle, l.Site)
		default:
			res, errExec = tx.Exec(
				fmt.Sprintf(`INSERT INTO %s (code, libelle) VALUES (?, ?)`, spec.Table),
				l.Code, l.Libelle)
		}
		if errExec != nil {
			err = fmt.Errorf("insertion %s « %s » (ligne %d) : %w", spec.Libelle, l.Code, l.NumeroLigne, errExec)
			return nil, err
		}
		id, _ := res.LastInsertId()
		if err = d.JournaliserCreation(tx, spec.Libelle, spec.Table, id); err != nil {
			return nil, err
		}
		comptes[l.Type]++
	}

	if errCommit := tx.Commit(); errCommit != nil {
		err = fmt.Errorf("validation de la transaction d'import des référentiels : %w", errCommit)
		return nil, err
	}
	return comptes, nil
}

// ---------------------------------------------------------------- écran

// compteReferentiel est une ligne du résumé « N projets, M zones… ».
type compteReferentiel struct {
	Libelle string
	Nb      int
}

// rapportImportReferentiels est le contexte du gabarit
// "import_referentiels_resultat" — mêmes états que rapportImportModeles.
type rapportImportReferentiels struct {
	MessageJeton string
	NbLignes     int
	Erreurs      []importation.ErreurLigne
	Jeton        string
	Comptes      []compteReferentiel
	Ecrit        bool
}

func resumerReferentiels(valides []ligneReferentielImport) []compteReferentiel {
	comptes := map[string]int{}
	for _, l := range valides {
		comptes[l.Type]++
	}
	return comptesEnListe(comptes)
}

func comptesEnListe(comptes map[string]int) []compteReferentiel {
	var out []compteReferentiel
	for _, t := range ordreTypesReferentiels {
		if n := comptes[t]; n > 0 {
			out = append(out, compteReferentiel{Libelle: typesReferentielsImport[t].Libelle, Nb: n})
		}
	}
	return out
}

func (s *serveur) analyserFichierReferentiels(contenu []byte) (importation.Rapport, []ligneReferentielImport, error) {
	entetes, lignes, numeros, err := importation.LireCSV(bytes.NewReader(contenu))
	if err != nil {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: err.Error()}}}, nil, nil
	}
	return validerLignesReferentiels(s.depot, entetes, lignes, numeros)
}

func (s *serveur) importReferentielsAnalyser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
			Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: "aucun fichier reçu"}},
		})
		return
	}
	defer fichier.Close()

	contenu, err := io.ReadAll(fichier)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	rapport, valides, err := s.analyserFichierReferentiels(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	jeton, err := importsEnAttente.Deposer(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
		NbLignes: rapport.NbLignes, Jeton: jeton, Comptes: resumerReferentiels(valides),
	})
}

func (s *serveur) importReferentielsConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
			MessageJeton: "Cet import a expiré ou a déjà été traité — réanalysez le fichier.",
		})
		return
	}
	defer importsEnAttente.Consommer(jeton)

	rapport, valides, err := s.analyserFichierReferentiels(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	comptes, err := executerImportReferentiels(s.depotPour(r), valides)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_referentiels_resultat", rapportImportReferentiels{
		NbLignes: rapport.NbLignes, Ecrit: true, Comptes: comptesEnListe(comptes),
	})
}
