const fs = require('fs');
const readline = require('readline');

const SCHEDULE_API_URL = "http://localhost:8000/api/v1/schedule/import";
const DOCS_API_URL = "http://localhost:8000/api/v1/laventecare/documents/seed";
const API_KEY = "homeapp-local-dev-2026-change-in-prod";

async function loadJsonl(filepath) {
    const data = [];
    if (!fs.existsSync(filepath)) {
        return data;
    }
    
    const fileStream = fs.createReadStream(filepath);
    const rl = readline.createInterface({
        input: fileStream,
        crlfDelay: Infinity
    });

    for await (const line of rl) {
        if (line.trim()) {
            data.push(JSON.parse(line));
        }
    }
    return data;
}

async function importSchedule() {
    const schedulePath = "C:\\Users\\jeffrey\\Desktop\\Projecten\\JeffriesHomeapp\\tmp_convex\\export\\schedule\\documents.jsonl";
    const data = await loadJsonl(schedulePath);
    
    if (data.length === 0) {
        console.log("❌ Geen schedule documenten gevonden of pad onjuist.");
        return;
    }

    console.log(`Gevonden: ${data.length} schedule documenten in Convex export.`);
    
    const rows = [];
    let userId = "user_3Ax561ZvuSkGtWpKFooeY65HNtY";
    
    for (const d of data) {
        if (d.userId) userId = d.userId;
        
        rows.push({
            eventId: d.eventId || "",
            titel: d.titel || "Dienst",
            startDatum: d.startDatum || "",
            startTijd: d.startTijd || "",
            eindDatum: d.eindDatum || "",
            eindTijd: d.eindTijd || "",
            werktijd: d.werktijd || "",
            locatie: d.locatie || "",
            team: String(d.team || "").substring(0, 20),
            shiftType: String(d.shiftType || "").substring(0, 20),
            prioriteit: Number(d.prioriteit) || 0,
            duur: parseFloat(d.duur) || 0.0,
            weeknr: String(d.weeknr || "").substring(0, 20),
            dag: String(d.dag || "").substring(0, 20),
            status: String(d.status || "").substring(0, 20),
            beschrijving: d.beschrijving || "",
            heledag: Boolean(d.heledag)
        });
    }
    
    const payload = {
        userId: userId,
        fileName: "Convex Historie Export",
        rows: rows
    };

    console.log("Versturen schedule naar Go Backend...");
    try {
        const response = await fetch(SCHEDULE_API_URL, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-API-Key': API_KEY
            },
            body: JSON.stringify(payload)
        });
        
        if (response.ok) {
            const result = await response.json();
            console.log("✅ Schedule Import succesvol:", result);
        } else {
            console.log("❌ Schedule Import mislukt:", await response.text());
        }
    } catch (e) {
        console.log("Fout bij versturen:", e.message);
    }
}

async function importDocs() {
    const docsPath = "C:\\Users\\jeffrey\\Desktop\\Projecten\\JeffriesHomeapp\\tmp_convex\\export\\laventecareDocuments\\documents.jsonl";
    const data = await loadJsonl(docsPath);
    
    if (data.length === 0) {
        console.log("❌ Geen laventecare documenten gevonden of pad onjuist.");
        return;
    }

    console.log(`\nGevonden: ${data.length} LaventeCare documenten in Convex export.`);
    
    const docsToSeed = [];
    let userId = "user_3Ax561ZvuSkGtWpKFooeY65HNtY";
    
    for (const d of data) {
        if (d.userId) userId = d.userId;
        
        docsToSeed.push({
            document_key: d.documentKey || "",
            titel: d.titel || "",
            samenvatting: d.samenvatting || "",
            categorie: d.categorie || "",
            fase: d.fase || "",
            versie: d.versie || "",
            source_path: d.sourcePath || "",
            tags: d.tags || [],
            user_id: userId,
            // we will let the backend create the timestamp
        });
    }
    
    console.log("Versturen LaventeCare documenten naar Go Backend...");
    try {
        const response = await fetch(DOCS_API_URL, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-API-Key': API_KEY
            },
            body: JSON.stringify(docsToSeed) // directly send the array!
        });
        
        if (response.ok) {
            const result = await response.json();
            console.log("✅ Documents Seed succesvol:", result);
        } else {
            console.log("❌ Documents Seed mislukt:", await response.text());
        }
    } catch (e) {
        console.log("Fout bij versturen:", e.message);
    }
}

async function main() {
    await importSchedule();
    await importDocs();
}

main();
