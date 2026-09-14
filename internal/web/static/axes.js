// Sélecteur d'axes par étiquettes (templates/composants/selecteur_axes.html).
// Sans dépendance : glisser-déposer HTML5 natif, clic en solution de repli
// (et au clavier). À chaque changement, les champs cachés axe1 … axeN sont
// régénérés dans l'ordre des étiquettes de la zone.
(function () {
  function init(racine) {
    if (racine.dataset.init) return;
    racine.dataset.init = "1";
    var zone = racine.querySelector(".axes-zone");
    var dispo = racine.querySelector(".axes-dispo");
    var champs = racine.querySelector(".axes-champs");
    var max = parseInt(racine.dataset.max, 10) || 6;
    // data-champ : un champ multi-valeurs (colonnes) ; sinon axe1 … axeN
    var nomChamp = racine.dataset.champ || "";

    function puces() { return zone.querySelectorAll(".puce"); }

    function maj() {
      champs.innerHTML = "";
      puces().forEach(function (p, i) {
        var champ = document.createElement("input");
        champ.type = "hidden";
        champ.name = nomChamp || ("axe" + (i + 1));
        champ.value = p.dataset.code;
        champs.appendChild(champ);
      });
      zone.classList.toggle("vide", puces().length === 0);
    }

    function ajouter(p, avant) {
      if (p.parentNode !== zone && puces().length >= max) return;
      if (avant && avant !== p && avant.parentNode === zone) {
        zone.insertBefore(p, avant);
      } else {
        zone.appendChild(p);
      }
      p.classList.add("choisi");
      maj();
    }

    function retirer(p) {
      dispo.appendChild(p);
      p.classList.remove("choisi");
      maj();
    }

    racine.addEventListener("click", function (e) {
      var p = e.target.closest(".puce");
      if (!p || !racine.contains(p)) return;
      if (p.parentNode === zone) retirer(p); else ajouter(p, null);
    });

    var glisse = null;
    racine.addEventListener("dragstart", function (e) {
      var p = e.target.closest(".puce");
      if (!p) return;
      glisse = p;
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", p.dataset.code);
      p.classList.add("en-vol");
    });
    racine.addEventListener("dragend", function () {
      if (glisse) glisse.classList.remove("en-vol");
      glisse = null;
      zone.classList.remove("survol");
    });
    [zone, dispo].forEach(function (cible) {
      cible.addEventListener("dragover", function (e) {
        if (!glisse) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        if (cible === zone) zone.classList.add("survol");
      });
      cible.addEventListener("dragleave", function () { zone.classList.remove("survol"); });
      cible.addEventListener("drop", function (e) {
        if (!glisse) return;
        e.preventDefault();
        zone.classList.remove("survol");
        if (cible === dispo) { retirer(glisse); return; }
        // point de dépôt : avant ou après l'étiquette survolée, selon la
        // moitié où le curseur se trouve
        var voisine = e.target.closest(".puce");
        var avant = null;
        if (voisine && voisine !== glisse && voisine.parentNode === zone) {
          var r = voisine.getBoundingClientRect();
          avant = (e.clientX - r.left) < r.width / 2 ? voisine : voisine.nextElementSibling;
        }
        ajouter(glisse, avant);
      });
    });

    maj();
  }

  function tous() { document.querySelectorAll(".axes-selecteur").forEach(init); }
  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", tous);
  } else {
    tous();
  }
  document.addEventListener("htmx:load", tous);
})();
