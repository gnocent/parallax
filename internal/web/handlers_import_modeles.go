package web

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"parallax/internal/depot"
	"parallax/internal/importation"
)

// routesImportModeles enregistre l'écran de reprise du catalogue matériel
// (v1.4 du backlog) : une page d'explication du format, une analyse en mode
// simulation (jamais d'écriture) et une confirmation qui écrit tout ou rien
// dans une transaction unique. Voir le commentaire en tête de
// handlers_import_modeles_test.go pour le scénario de bout en bout.
//
// La lecture (GET) est ouverte à tout lecteur connecté ; l'analyse et la
// confirmation, qui simulent puis exécutent une écriture, sont réservées à
// s.editeur comme toute mutation de l'application.
//
// Câblage : server.go n'est pas modifié par cet écran (consigne explicite) ;
// ajouter l'appel suivant dans (*serveur).routes(), aux côtés des autres
// routesXxx() :
//
//	s.routesImportModeles()
func (s *serveur) routesImportModeles() {
	s.mux.HandleFunc("GET /import/modeles", s.lecteur(s.importModelesPage))
	s.mux.HandleFunc("POST /import/modeles/analyser", s.editeur(s.importModelesAnalyser))
	s.mux.HandleFunc("POST /import/modeles/confirmer", s.editeur(s.importModelesConfirmer))
}

// ---------------------------------------------------------------- format du fichier

// specComposantImport décrit les colonnes CSV facultatives produisant un
// composant de révision, pour un code de depot.ComposantsCanoniques. Une
// évolution du vocabulaire ajoute une entrée ici (et dans
// ComposantsCanoniques), pas une colonne générique (CLAUDE.md, « Hors
// périmètre » — pas de champs libres).
//
// Trois formes :
//   - ColQuantite vide : valeur scalaire, la colonne ColCapacite porte le
//     total du serveur (cœurs, score, Go) et la quantité vaut 1 ;
//   - ColCapacite vide : simple dénombrement (gpu), la capacité vaut 1 ;
//   - les deux : une paire quantité / capacité unitaire, à renseigner
//     ensemble — sauf QuantitePartagee, où la quantité appartient à un
//     autre composant (gpu_nb pour les attributs GPU) : elle peut alors
//     être présente sans capacité, mais pas l'inverse.
type specComposantImport struct {
	ColQuantite      string
	ColCapacite      string
	QuantitePartagee bool
	Code             string
}

var specsComposantsImport = []specComposantImport{
	{ColCapacite: "cpu_coeurs", Code: "cpu"},
	{ColCapacite: "specrate", Code: "specrate"},
	{ColCapacite: "phoronix", Code: "phoronix"},
	{ColCapacite: "ram_go", Code: "ram"},
	{ColQuantite: "hdd_nb", ColCapacite: "hdd_to", Code: "hdd"},
	{ColQuantite: "ssd_nb", ColCapacite: "ssd_to", Code: "ssd"},
	{ColQuantite: "nic_nb", ColCapacite: "nic_gbps", Code: "nic"},
	{ColQuantite: "gpu_nb", Code: "gpu"},
	{ColQuantite: "gpu_nb", ColCapacite: "gpu_ram_go", QuantitePartagee: true, Code: "gpu_ram"},
	{ColQuantite: "gpu_nb", ColCapacite: "gpu_tflops_fp8", QuantitePartagee: true, Code: "gpu_fp8"},
	{ColQuantite: "gpu_nb", ColCapacite: "gpu_bp_tos", QuantitePartagee: true, Code: "gpu_bp"},
}

// canonique retrouve la définition (nature, unité) d'un code de composant.
func canonique(code string) depot.ComposantCanonique {
	for _, c := range depot.ComposantsCanoniques {
		if c.Code == code {
			return c
		}
	}
	panic("composant d'import sans définition canonique : " + code)
}

// lireComposantImport applique une spec à une ligne : (composant, présent,
// message d'erreur). Absent et sans erreur quand aucune de ses colonnes
// n'est renseignée.
func lireComposantImport(spec specComposantImport, ligne map[string]string) (composantAImporter, bool, string) {
	var qTexte, cTexte string
	if spec.ColQuantite != "" {
		qTexte = strings.TrimSpace(ligne[spec.ColQuantite])
	}
	if spec.ColCapacite != "" {
		cTexte = strings.TrimSpace(ligne[spec.ColCapacite])
	}
	if qTexte == "" && cTexte == "" {
		return composantAImporter{}, false, ""
	}

	switch {
	case spec.ColQuantite == "":
		// scalaire : rien d'autre à vérifier, qTexte est vide par construction
	case spec.ColCapacite == "":
		// dénombrement : idem, cTexte vide
	case cTexte == "" && spec.QuantitePartagee:
		return composantAImporter{}, false, "" // la quantité sert à un autre composant
	case qTexte == "" || cTexte == "":
		return composantAImporter{}, false, fmt.Sprintf("%s : %s et %s doivent être renseignées ensemble", spec.Code, spec.ColQuantite, spec.ColCapacite)
	}

	quantite, capacite := 1.0, 1.0
	if qTexte != "" {
		q, err := importation.ParserDecimal(qTexte)
		if err != nil {
			return composantAImporter{}, false, fmt.Sprintf("%s : %s", spec.ColQuantite, err.Error())
		}
		if *q <= 0 {
			return composantAImporter{}, false, fmt.Sprintf("%s : %s doit être strictement positive", spec.Code, spec.ColQuantite)
		}
		quantite = *q
	}
	if cTexte != "" {
		c, err := importation.ParserDecimal(cTexte)
		if err != nil {
			return composantAImporter{}, false, fmt.Sprintf("%s : %s", spec.ColCapacite, err.Error())
		}
		if *c <= 0 {
			return composantAImporter{}, false, fmt.Sprintf("%s : %s doit être strictement positive", spec.Code, spec.ColCapacite)
		}
		capacite = *c
	}
	def := canonique(spec.Code)
	return composantAImporter{
		Nature: def.Nature, Code: def.Code, Unite: def.Unite,
		Quantite: quantite, CapaciteUnitaire: capacite,
	}, true, ""
}

var entetesObligatoiresImportModeles = []string{"type", "annee", "code"}

// ---------------------------------------------------------------- types de travail

// composantAImporter est un composant déjà validé, prêt à être écrit tel
// quel — les colonnes sources n'ont plus besoin d'être portées au-delà de la
// validation.
type composantAImporter struct {
	Nature           string
	Code             string
	Quantite         float64
	CapaciteUnitaire float64
	Unite            string
}

// ligneModeleImport est une ligne de fichier entièrement validée, prête pour
// l'écriture. Une ligne qui échoue la validation n'atteint jamais ce type :
// elle produit une importation.ErreurLigne à la place.
type ligneModeleImport struct {
	NumeroLigne       int
	Type              string
	Annee             int64
	Code              string
	Description       *string
	ModeFinancement   *string
	DureeLeaseMois    *int64
	DateDebutLease    *string
	PrixFournisseurHT *float64
	CoutAnnuelHT      *float64
	DureeCoutAnnees   *int64
	Composants        []composantAImporter
}

// ---------------------------------------------------------------- validation

// chaineOptionnelleCSV renvoie nil pour une valeur de cellule vide (après
// suppression des espaces de tête/fin, déjà faite par importation.LireCSV),
// sinon un pointeur vers la valeur — même convention que
// chaineOptionnelleModele (handlers_modele.go) pour les champs facultatifs.
func chaineOptionnelleCSV(valeur string) *string {
	if valeur == "" {
		return nil
	}
	v := valeur
	return &v
}

// joindreNumeros formate une liste de numéros de ligne pour un message
// d'erreur, par ex. « 3 et 5 » ou « 3, 5 et 9 ».
func joindreNumeros(numeros []int) string {
	textes := make([]string, len(numeros))
	for i, n := range numeros {
		textes[i] = fmt.Sprintf("%d", n)
	}
	if len(textes) <= 1 {
		return strings.Join(textes, "")
	}
	return strings.Join(textes[:len(textes)-1], ", ") + " et " + textes[len(textes)-1]
}

// validerLignesModeles exécute la passe de validation complète décrite dans
// CLAUDE.md et docs/backlog.md v1.4 : toutes les anomalies du fichier sont
// remontées ensemble, jamais une seule à la fois. Le second retour ne contient
// que les lignes valides, dans l'ordre du fichier ; il est ignoré par
// l'appelant dès que le rapport porte au moins une erreur.
//
// Le troisième retour est une erreur *inattendue* (panne de la base lors de
// LireModeleParCode, par exemple) — à distinguer d'une anomalie de saisie,
// qui va dans le rapport. Cette fonction est appelée deux fois pour un même
// import (analyse, puis revalidation à la confirmation) : la base a pu changer
// entretemps, ce qui est précisément voulu.
func validerLignesModeles(d *depot.Depot, entetes []string, lignes []map[string]string, numeros []int) (importation.Rapport, []ligneModeleImport, error) {
	manquantes := importation.EntetesManquantes(entetes, entetesObligatoiresImportModeles)
	if len(manquantes) > 0 {
		return importation.Rapport{
			Erreurs: []importation.ErreurLigne{{
				Ligne:   1,
				Message: fmt.Sprintf("colonne(s) obligatoire(s) manquante(s) : %s", strings.Join(manquantes, ", ")),
			}},
		}, nil, nil
	}

	// numéros de ligne partageant un même code, pour détecter les doublons
	// intra-fichier avant même de valider ligne par ligne.
	lignesParCode := map[string][]int{}
	for i, ligne := range lignes {
		code := strings.TrimSpace(ligne["code"])
		if code != "" {
			lignesParCode[code] = append(lignesParCode[code], numeros[i])
		}
	}

	var erreurs []importation.ErreurLigne
	var valides []ligneModeleImport

	for i, ligne := range lignes {
		numero := numeros[i]
		var messages []string

		typeVal := strings.TrimSpace(ligne["type"])
		if typeVal == "" {
			messages = append(messages, "type vide")
		}

		code := strings.TrimSpace(ligne["code"])
		if code == "" {
			messages = append(messages, "code vide")
		} else if len(lignesParCode[code]) > 1 {
			messages = append(messages, fmt.Sprintf("code « %s » dupliqué dans le fichier (lignes %s)", code, joindreNumeros(lignesParCode[code])))
		} else {
			_, err := d.LireModeleParCode(code)
			switch {
			case err == nil:
				messages = append(messages, fmt.Sprintf("code « %s » déjà utilisé par un modèle existant", code))
			case errors.Is(err, depot.ErrIntrouvable):
				// libre, c'est le cas attendu.
			default:
				return importation.Rapport{}, nil, err
			}
		}

		var annee int64
		anneeTexte := strings.TrimSpace(ligne["annee"])
		if anneeTexte == "" {
			messages = append(messages, "annee obligatoire")
		} else if v, err := importation.ParserEntier(anneeTexte); err != nil {
			messages = append(messages, err.Error())
		} else if *v < 2000 || *v > 2100 {
			messages = append(messages, fmt.Sprintf("annee « %d » hors plage plausible 2000-2100", *v))
		} else {
			annee = *v
		}

		var modeFinancement *string
		modeFinTexte := strings.TrimSpace(ligne["mode_financement"])
		if modeFinTexte != "" {
			if modeFinTexte != "ACHAT" && modeFinTexte != "LEASE" {
				messages = append(messages, fmt.Sprintf("mode_financement « %s » doit être ACHAT ou LEASE", modeFinTexte))
			} else {
				modeFinancement = &modeFinTexte
			}
		}

		dureeLeaseMois, err := importation.ParserEntier(ligne["duree_lease_mois"])
		if err != nil {
			messages = append(messages, "duree_lease_mois : "+err.Error())
		}

		dateDebutLease := chaineOptionnelleCSV(strings.TrimSpace(ligne["date_debut_lease"]))
		if dateDebutLease != nil {
			if _, err := time.Parse("2006-01-02", *dateDebutLease); err != nil {
				messages = append(messages, fmt.Sprintf("date_debut_lease « %s » n'est pas au format YYYY-MM-DD", *dateDebutLease))
			}
		}

		prixFournisseurHT, err := importation.ParserDecimal(ligne["prix_fournisseur_ht"])
		if err != nil {
			messages = append(messages, "prix_fournisseur_ht : "+err.Error())
		}

		coutAnnuelHT, err := importation.ParserDecimal(ligne["cout_annuel_ht"])
		if err != nil {
			messages = append(messages, "cout_annuel_ht : "+err.Error())
		}

		dureeCoutAnnees, err := importation.ParserEntier(ligne["duree_cout_annees"])
		if err != nil {
			messages = append(messages, "duree_cout_annees : "+err.Error())
		}

		var composants []composantAImporter
		for _, spec := range specsComposantsImport {
			comp, present, message := lireComposantImport(spec, ligne)
			if message != "" {
				messages = append(messages, message)
				continue
			}
			if present {
				composants = append(composants, comp)
			}
		}

		if len(messages) > 0 {
			erreurs = append(erreurs, importation.ErreurLigne{Ligne: numero, Message: strings.Join(messages, " ; ")})
			continue
		}

		valides = append(valides, ligneModeleImport{
			NumeroLigne:       numero,
			Type:              typeVal,
			Annee:             annee,
			Code:              code,
			Description:       chaineOptionnelleCSV(strings.TrimSpace(ligne["description"])),
			ModeFinancement:   modeFinancement,
			DureeLeaseMois:    dureeLeaseMois,
			DateDebutLease:    dateDebutLease,
			PrixFournisseurHT: prixFournisseurHT,
			CoutAnnuelHT:      coutAnnuelHT,
			DureeCoutAnnees:   dureeCoutAnnees,
			Composants:        composants,
		})
	}

	return importation.Rapport{NbLignes: len(lignes), Erreurs: erreurs}, valides, nil
}

// ---------------------------------------------------------------- écriture

// executerImportModeles écrit toutes les lignes valides dans une transaction
// unique ouverte sur la connexion partagée du dépôt (Depot.Base(), déjà
// utilisée hors du dépôt dans handlers_dimensionnement.go). C'est le seul
// moyen d'obtenir un « tout ou rien » sur l'ensemble du fichier : les méthodes
// d'écriture du dépôt (CreerModele, CreerRevision, AjouterComposant) ouvrent
// chacune leur propre transaction et ne peuvent pas être enchaînées dans une
// transaction partagée par l'appelant (voir le commentaire de tête du fichier
// de tâche pour la justification complète).
//
// Toute erreur SQL (ne devrait pas arriver après validerLignesModeles, mais
// défensif comme demandé) déclenche un rollback complet : zéro ligne créée.
func executerImportModeles(d *depot.Depot, valides []ligneModeleImport) (nbModeles, nbRevisions, nbComposants int, err error) {
	tx, err := d.Base().Begin()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ouverture de la transaction d'import : %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	aujourdhui := time.Now().Format("2006-01-02")

	for _, l := range valides {
		res, errExec := tx.Exec(
			`INSERT INTO modele (type, annee, code, description, mode_financement,
				duree_lease_mois, date_debut_lease, prix_fournisseur_ht,
				cout_annuel_ht, duree_cout_annees)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.Type, l.Annee, l.Code, l.Description, l.ModeFinancement,
			l.DureeLeaseMois, l.DateDebutLease, l.PrixFournisseurHT,
			l.CoutAnnuelHT, l.DureeCoutAnnees)
		if errExec != nil {
			err = fmt.Errorf("insertion du modèle « %s » (ligne %d) : %w", l.Code, l.NumeroLigne, errExec)
			return 0, 0, 0, err
		}
		modeleID, _ := res.LastInsertId()
		if err = d.JournaliserCreation(tx, "modele", "modele", modeleID); err != nil {
			return 0, 0, 0, err
		}
		nbModeles++

		if len(l.Composants) == 0 {
			continue
		}

		dateEffet := aujourdhui
		if l.DateDebutLease != nil {
			dateEffet = *l.DateDebutLease
		}
		libelle := "import initial"
		resRevision, errExec := tx.Exec(
			`INSERT INTO revision (modele_id, numero, libelle, date_effet) VALUES (?, 1, ?, ?)`,
			modeleID, libelle, dateEffet)
		if errExec != nil {
			err = fmt.Errorf("création de la révision du modèle « %s » (ligne %d) : %w", l.Code, l.NumeroLigne, errExec)
			return 0, 0, 0, err
		}
		revisionID, _ := resRevision.LastInsertId()
		if err = d.JournaliserCreation(tx, "revision", "revision", revisionID); err != nil {
			return 0, 0, 0, err
		}
		nbRevisions++

		for _, c := range l.Composants {
			resComp, errExec := tx.Exec(
				`INSERT INTO composant (revision_id, nature, code, quantite, capacite_unitaire, unite)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				revisionID, c.Nature, c.Code, c.Quantite, c.CapaciteUnitaire, c.Unite)
			if errExec != nil {
				err = fmt.Errorf("insertion du composant « %s » du modèle « %s » (ligne %d) : %w", c.Code, l.Code, l.NumeroLigne, errExec)
				return 0, 0, 0, err
			}
			compID, _ := resComp.LastInsertId()
			if err = d.JournaliserCreation(tx, "composant", "composant", compID); err != nil {
				return 0, 0, 0, err
			}
			nbComposants++
		}
	}

	if errCommit := tx.Commit(); errCommit != nil {
		err = fmt.Errorf("validation de la transaction d'import : %w", errCommit)
		return 0, 0, 0, err
	}
	return nbModeles, nbRevisions, nbComposants, nil
}

// ---------------------------------------------------------------- écran

// rapportImportModeles est le contexte du gabarit "import_modeles_resultat".
// Un seul type sert les trois issues possibles :
//   - MessageJeton non vide  : jeton absent ou expiré à la confirmation
//   - Erreurs non vide       : au moins une ligne invalide (simulation ou
//     revalidation), rien n'a été écrit
//   - Ecrit vrai             : import réel effectué, NbCreees/NbRevisions/
//     NbComposants décrivent ce qui a été créé
//   - sinon                  : résultat de simulation réussie, Jeton porte le
//     jeton à republier vers /import/modeles/confirmer
type rapportImportModeles struct {
	MessageJeton string
	NbLignes     int
	Erreurs      []importation.ErreurLigne
	Jeton        string
	NbModeles    int
	NbRevisions  int
	NbComposants int
	Ecrit        bool
	NbCreees     int
}

func (s *serveur) importModelesPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_modeles"), "import_modeles_page", nil)
}

// analyserFichierModeles décode et valide un contenu CSV, sans jamais écrire
// — c'est la brique commune à l'analyse (toujours en simulation) et à la
// confirmation (revalidation avant écriture réelle).
func (s *serveur) analyserFichierModeles(contenu []byte) (importation.Rapport, []ligneModeleImport, error) {
	entetes, lignes, numeros, err := importation.LireCSV(bytes.NewReader(contenu))
	if err != nil {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: err.Error()}}}, nil, nil
	}
	return validerLignesModeles(s.depot, entetes, lignes, numeros)
}

// compterRevisionsEtComposants résume les révisions et composants qu'un
// import créerait, pour le message de simulation (« N modèles prêts, M
// révisions, P composants ») sans dupliquer la logique d'écriture.
func compterRevisionsEtComposants(valides []ligneModeleImport) (nbRevisions, nbComposants int) {
	for _, l := range valides {
		if len(l.Composants) > 0 {
			nbRevisions++
			nbComposants += len(l.Composants)
		}
	}
	return nbRevisions, nbComposants
}

// importModelesAnalyser exécute la simulation : jamais d'écriture. En
// l'absence d'erreur, le fichier est déposé dans importsEnAttente et son
// jeton porté par le formulaire de confirmation rendu dans le fragment.
func (s *serveur) importModelesAnalyser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
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

	rapport, valides, err := s.analyserFichierModeles(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}

	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}

	jeton, err := importsEnAttente.Deposer(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	nbRevisions, nbComposants := compterRevisionsEtComposants(valides)
	s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
		NbLignes: rapport.NbLignes, Jeton: jeton,
		NbModeles: len(valides), NbRevisions: nbRevisions, NbComposants: nbComposants,
	})
}

// importModelesConfirmer récupère le fichier déposé sous le jeton, le
// revalide entièrement (l'état de la base a pu changer depuis l'analyse), et
// n'écrit que si la revalidation ne remonte toujours aucune erreur. Le jeton
// est consommé dans tous les cas dès qu'il a été trouvé — un jeton ne sert
// qu'une fois.
func (s *serveur) importModelesConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
			MessageJeton: "Cet import a expiré ou a déjà été traité — réanalysez le fichier.",
		})
		return
	}
	defer importsEnAttente.Consommer(jeton)

	rapport, valides, err := s.analyserFichierModeles(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}

	nbModeles, nbRevisions, nbComposants, err := executerImportModeles(s.depotPour(r), valides)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_modeles_resultat", rapportImportModeles{
		NbLignes: rapport.NbLignes, Ecrit: true,
		NbCreees: nbModeles, NbRevisions: nbRevisions, NbComposants: nbComposants,
	})
}
