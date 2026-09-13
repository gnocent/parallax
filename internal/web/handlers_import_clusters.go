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

// routesImportClusters enregistre l'import des clusters : une ligne par
// cluster, dimensions désignées par leur code (référentiels déjà en base —
// voir /import/referentiels). Même discipline que les autres imports :
// analyse sans écriture, confirmation dans une transaction unique, un
// cluster déjà existant (même projet, environnement et nom) est une erreur
// de ligne.
func (s *serveur) routesImportClusters() {
	s.mux.HandleFunc("GET /import/clusters", s.lecteur(s.importClustersPage))
	s.mux.HandleFunc("POST /import/clusters/analyser", s.editeur(s.importClustersAnalyser))
	s.mux.HandleFunc("POST /import/clusters/confirmer", s.editeur(s.importClustersConfirmer))
}

func (s *serveur) importClustersPage(w http.ResponseWriter, r *http.Request) {
	s.rendrePage(w, r, s.titre(r, "titre.import_clusters"), "import_clusters_page", nil)
}

var entetesObligatoiresImportClusters = []string{"nom", "projet_code", "environnement_code", "techno_code"}

// ligneClusterImport est une ligne validée, références résolues.
type ligneClusterImport struct {
	NumeroLigne int
	Cluster     depot.Cluster
}

// cachesImportClusters évite de relire le même référentiel à chaque ligne —
// un fichier de clusters répète les mêmes codes projet et environnement des
// dizaines de fois. Une entrée nil signifie « code inexistant, déjà
// constaté ».
type cachesImportClusters struct {
	projets        map[string]*int64
	environnements map[string]*int64
	technos        map[string]*int64
	tiers          map[string]*int64
	usages         map[string]*int64
}

func nouvellesCachesImportClusters() *cachesImportClusters {
	return &cachesImportClusters{
		projets: map[string]*int64{}, environnements: map[string]*int64{}, technos: map[string]*int64{},
		tiers: map[string]*int64{}, usages: map[string]*int64{},
	}
}

// resoudreCodeImport lit un référentiel par code, mémorise le résultat et
// renvoie l'identifiant, ou nil si le code n'existe pas. Une erreur est une
// panne, jamais un code inconnu.
func resoudreCodeImport(cache map[string]*int64, code string, lire func(string) (int64, error)) (*int64, error) {
	if id, vu := cache[code]; vu {
		return id, nil
	}
	id, err := lire(code)
	switch {
	case err == nil:
		cache[code] = &id
		return &id, nil
	case errors.Is(err, depot.ErrIntrouvable):
		cache[code] = nil
		return nil, nil
	default:
		return nil, err
	}
}

// clusterExiste vérifie l'unicité (projet, environnement, nom) contre la
// base, qu'il s'agisse d'un cluster actif ou non : l'unicité SQL ne
// distingue pas les deux.
func clusterExiste(d *depot.Depot, projetID, environnementID int64, nom string) (bool, error) {
	var id int64
	err := d.Base().QueryRow(
		`SELECT id FROM cluster WHERE projet_id = ? AND environnement_id = ? AND nom = ?`,
		projetID, environnementID, nom).Scan(&id)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("recherche du cluster « %s » : %w", nom, err)
	}
}

// validerLignesClusters remonte toutes les anomalies ensemble ; le second
// retour n'a de sens que sans erreur ; le troisième est une panne.
func (s *serveur) validerLignesClusters(entetes []string, lignes []map[string]string, numeros []int) (importation.Rapport, []ligneClusterImport, error) {
	manquantes := importation.EntetesManquantes(entetes, entetesObligatoiresImportClusters)
	if len(manquantes) > 0 {
		return importation.Rapport{
			Erreurs: []importation.ErreurLigne{{
				Ligne:   1,
				Message: fmt.Sprintf("colonne(s) obligatoire(s) manquante(s) : %s", strings.Join(manquantes, ", ")),
			}},
		}, nil, nil
	}

	// doublons intra-fichier sur (projet, environnement, nom)
	lignesParCle := map[string][]int{}
	cleDe := func(ligne map[string]string) string {
		return strings.TrimSpace(ligne["projet_code"]) + "\x1f" + strings.TrimSpace(ligne["environnement_code"]) + "\x1f" + strings.TrimSpace(ligne["nom"])
	}
	for i, ligne := range lignes {
		lignesParCle[cleDe(ligne)] = append(lignesParCle[cleDe(ligne)], numeros[i])
	}

	caches := nouvellesCachesImportClusters()
	d := s.depot
	var erreurs []importation.ErreurLigne
	var valides []ligneClusterImport

	for i, ligne := range lignes {
		numero := numeros[i]
		var messages []string

		nom := strings.TrimSpace(ligne["nom"])
		if nom == "" {
			messages = append(messages, "nom vide")
		}

		// resoudre lit une dimension par code ; obligatoire signale un code
		// vide comme anomalie, sinon le vide vaut « non renseigné ». Un code
		// inconnu est une anomalie de ligne ; une erreur de lecture, une panne.
		resoudre := func(colonne, libelle string, obligatoire bool, cache map[string]*int64, lire func(string) (int64, error)) (*int64, error) {
			code := strings.TrimSpace(ligne[colonne])
			if code == "" {
				if obligatoire {
					messages = append(messages, colonne+" vide")
				}
				return nil, nil
			}
			id, err := resoudreCodeImport(cache, code, lire)
			if err != nil {
				return nil, err
			}
			if id == nil {
				messages = append(messages, fmt.Sprintf("%s « %s » inconnu", libelle, code))
			}
			return id, nil
		}

		projetID, err := resoudre("projet_code", "projet", true, caches.projets,
			func(c string) (int64, error) { p, err := d.LireProjetParCode(c); return p.ID, err })
		if err != nil {
			return importation.Rapport{}, nil, err
		}
		environnementID, err := resoudre("environnement_code", "environnement", true, caches.environnements,
			func(c string) (int64, error) { e, err := d.LireEnvironnementParCode(c); return e.ID, err })
		if err != nil {
			return importation.Rapport{}, nil, err
		}
		technoID, err := resoudre("techno_code", "techno", true, caches.technos,
			func(c string) (int64, error) { t, err := d.LireTechnoParCode(c); return t.ID, err })
		if err != nil {
			return importation.Rapport{}, nil, err
		}
		tierID, err := resoudre("tier_code", "tier", false, caches.tiers,
			func(c string) (int64, error) { t, err := d.LireTierParCode(c); return t.ID, err })
		if err != nil {
			return importation.Rapport{}, nil, err
		}
		usageID, err := resoudre("usage_code", "usage", false, caches.usages,
			func(c string) (int64, error) { u, err := d.LireUsageParCode(c); return u.ID, err })
		if err != nil {
			return importation.Rapport{}, nil, err
		}

		if nom != "" && projetID != nil && environnementID != nil {
			if lignesDoublon := lignesParCle[cleDe(ligne)]; len(lignesDoublon) > 1 {
				messages = append(messages, fmt.Sprintf("cluster « %s » dupliqué dans le fichier (lignes %s)", nom, joindreNumeros(lignesDoublon)))
			} else {
				existe, err := clusterExiste(d, *projetID, *environnementID, nom)
				if err != nil {
					return importation.Rapport{}, nil, err
				}
				if existe {
					messages = append(messages, fmt.Sprintf("cluster « %s » existe déjà pour ce projet et cet environnement", nom))
				}
			}
		}

		if len(messages) > 0 {
			erreurs = append(erreurs, importation.ErreurLigne{Ligne: numero, Message: strings.Join(messages, " ; ")})
			continue
		}
		valides = append(valides, ligneClusterImport{
			NumeroLigne: numero,
			Cluster: depot.Cluster{
				Nom: nom, ProjetID: *projetID, EnvironnementID: *environnementID, TechnoID: *technoID,
				TierID: tierID, UsageFonctionnelID: usageID,
				Commentaire: texteOptionnelCSV(ligne["commentaire"]),
			},
		})
	}

	return importation.Rapport{NbLignes: len(lignes), Erreurs: erreurs}, valides, nil
}

// executerImportClusters écrit toutes les lignes dans une transaction
// unique sur Depot.Base() (même justification que les autres imports).
func executerImportClusters(d *depot.Depot, valides []ligneClusterImport) (int, error) {
	tx, err := d.Base().Begin()
	if err != nil {
		return 0, fmt.Errorf("ouverture de la transaction d'import des clusters : %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	n := 0
	for _, l := range valides {
		c := l.Cluster
		res, errExec := tx.Exec(
			`INSERT INTO cluster (nom, projet_id, environnement_id, techno_id, tier_id, usage_fonctionnel_id, commentaire)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			c.Nom, c.ProjetID, c.EnvironnementID, c.TechnoID, c.TierID, c.UsageFonctionnelID, c.Commentaire)
		if errExec != nil {
			err = fmt.Errorf("insertion du cluster « %s » (ligne %d) : %w", c.Nom, l.NumeroLigne, errExec)
			return 0, err
		}
		id, _ := res.LastInsertId()
		if err = d.JournaliserCreation(tx, "cluster", "cluster", id); err != nil {
			return 0, err
		}
		n++
	}
	if errCommit := tx.Commit(); errCommit != nil {
		err = fmt.Errorf("validation de la transaction d'import des clusters : %w", errCommit)
		return 0, err
	}
	return n, nil
}

// ---------------------------------------------------------------- écran

// rapportImportClusters est le contexte du gabarit "import_clusters_resultat".
type rapportImportClusters struct {
	MessageJeton string
	NbLignes     int
	Erreurs      []importation.ErreurLigne
	Jeton        string
	NbClusters   int
	Ecrit        bool
}

func (s *serveur) analyserFichierClusters(contenu []byte) (importation.Rapport, []ligneClusterImport, error) {
	entetes, lignes, numeros, err := importation.LireCSV(bytes.NewReader(contenu))
	if err != nil {
		return importation.Rapport{Erreurs: []importation.ErreurLigne{{Ligne: 0, Message: err.Error()}}}, nil, nil
	}
	return s.validerLignesClusters(entetes, lignes, numeros)
}

func (s *serveur) importClustersAnalyser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	fichier, _, err := r.FormFile("fichier")
	if err != nil {
		s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
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
	rapport, valides, err := s.analyserFichierClusters(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	jeton, err := importsEnAttente.Deposer(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
		NbLignes: rapport.NbLignes, Jeton: jeton, NbClusters: len(valides),
	})
}

func (s *serveur) importClustersConfirmer(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "formulaire illisible", http.StatusBadRequest)
		return
	}
	jeton := r.FormValue("jeton")
	contenu, ok := importsEnAttente.Recuperer(jeton)
	if !ok {
		s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
			MessageJeton: "Cet import a expiré ou a déjà été traité — réanalysez le fichier.",
		})
		return
	}
	defer importsEnAttente.Consommer(jeton)

	rapport, valides, err := s.analyserFichierClusters(contenu)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	if rapport.EnErreur() {
		s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
			NbLignes: rapport.NbLignes, Erreurs: rapport.Erreurs,
		})
		return
	}
	n, err := executerImportClusters(s.depotPour(r), valides)
	if err != nil {
		s.erreurServeur(w, r, err)
		return
	}
	s.rendreFragment(w, r, "import_clusters_resultat", rapportImportClusters{
		NbLignes: rapport.NbLignes, Ecrit: true, NbClusters: n,
	})
}
