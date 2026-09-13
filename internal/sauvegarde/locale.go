package sauvegarde

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"parallax/internal/db"
)

// Convention de nom des fichiers de sauvegarde locale — la même que celle du
// dépôt S3 (cmd/parallax/main.go, deposerSauvegardeS3), pour rester
// reconnaissable d'un mécanisme à l'autre. La date fait partie du nom en
// heure locale : c'est celle que l'administrateur règle dans l'écran de
// paramètres, et celle qu'il retrouve en la lisant.
const (
	prefixeFichierLocal = "parallax-"
	suffixeFichierLocal = ".db"
	formatHorodatageNom = "20060102-150405"
)

// EcrireLocale produit un fichier de sauvegarde horodaté dans dossier (créé
// si besoin) et renvoie son nom. VACUUM INTO (db.Sauvegarder) produit une
// copie cohérente sans interrompre les lecteurs en cours.
func EcrireLocale(base *sql.DB, dossier string) (nom string, err error) {
	if strings.TrimSpace(dossier) == "" {
		return "", fmt.Errorf("sauvegarde locale : dossier vide")
	}
	if err := os.MkdirAll(dossier, 0o750); err != nil {
		return "", fmt.Errorf("création du dossier de sauvegarde %s : %w", dossier, err)
	}
	nom = prefixeFichierLocal + time.Now().Format(formatHorodatageNom) + suffixeFichierLocal
	if err := db.Sauvegarder(base, filepath.Join(dossier, nom)); err != nil {
		return "", err
	}
	return nom, nil
}

// FichierLocal décrit une sauvegarde locale existante, pour l'affichage de
// l'écran de paramètres.
type FichierLocal struct {
	Nom       string
	Taille    int64
	ModifieLe time.Time
}

// ListerLocales renvoie les sauvegardes locales de dossier, la plus récente
// d'abord. Un dossier absent (fonctionnalité jamais utilisée, ou pas encore
// créée) n'est pas une erreur : une liste vide.
func ListerLocales(dossier string) ([]FichierLocal, error) {
	entrées, err := os.ReadDir(dossier)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("lecture du dossier de sauvegarde %s : %w", dossier, err)
	}
	var fichiers []FichierLocal
	for _, e := range entrées {
		if e.IsDir() || !estFichierSauvegarde(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fichiers = append(fichiers, FichierLocal{Nom: e.Name(), Taille: info.Size(), ModifieLe: info.ModTime()})
	}
	sort.Slice(fichiers, func(i, j int) bool { return fichiers[i].ModifieLe.After(fichiers[j].ModifieLe) })
	return fichiers, nil
}

// PurgerLocales supprime les sauvegardes locales de dossier plus vieilles
// que retention et renvoie leur nombre. Ne touche jamais un fichier hors de
// la convention de nom de EcrireLocale : ce dossier pourrait, en théorie,
// être partagé avec autre chose.
func PurgerLocales(dossier string, retention time.Duration) (int, error) {
	fichiers, err := ListerLocales(dossier)
	if err != nil {
		return 0, err
	}
	seuil := time.Now().Add(-retention)
	n := 0
	for _, f := range fichiers {
		if f.ModifieLe.Before(seuil) {
			if err := os.Remove(filepath.Join(dossier, f.Nom)); err != nil {
				return n, fmt.Errorf("suppression de %s : %w", f.Nom, err)
			}
			n++
		}
	}
	return n, nil
}

func estFichierSauvegarde(nom string) bool {
	return strings.HasPrefix(nom, prefixeFichierLocal) && strings.HasSuffix(nom, suffixeFichierLocal)
}
