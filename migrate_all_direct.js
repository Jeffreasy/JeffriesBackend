const fs = require('fs');
const readline = require('readline');
const { Client } = require('pg');

const client = new Client({
    connectionString: "postgresql://homeapp:change-me@postgres:5432/homeapp?sslmode=disable"
});

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

async function migrateTransactions() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/transactions/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen transacties gevonden");

    console.log(`Migrating ${data.length} transactions...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO transactions (
                    user_id, rekening_iban, volgnr, datum, bedrag, saldo_na_trn, code, 
                    tegenrekening_iban, tegenpartij_naam, omschrijving, referentie, is_interne_overboeking, categorie
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
                ON CONFLICT (user_id, rekening_iban, volgnr) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.rekeningIban || "",
                String(d.volgnr || ""),
                d.datum || new Date().toISOString().split('T')[0],
                d.bedrag || 0,
                d.saldoNaTrn || 0,
                d.code || "",
                d.tegenrekeningIban || null,
                d.tegenpartijNaam || null,
                d.omschrijving || "",
                d.referentie || null,
                Boolean(d.isInterneOverboeking),
                d.categorie || null
            ]);
            count++;
        } catch (e) {
            console.error("Fout transactie", e);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} transacties`);
}

async function migratePersonalEvents() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/personalEvents/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen personal events gevonden");

    console.log(`Migrating ${data.length} personal events...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO personal_events (
                    user_id, event_id, titel, start_datum, start_tijd, eind_datum, eind_tijd,
                    heledag, locatie, beschrijving, conflict_met_dienst, status, kalender
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
                ON CONFLICT (user_id, event_id) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.eventId || "",
                d.titel || "Event",
                d.startDatum || new Date().toISOString().split('T')[0],
                d.startTijd || null,
                d.eindDatum || new Date().toISOString().split('T')[0],
                d.eindTijd || null,
                Boolean(d.heledag),
                d.locatie || null,
                d.beschrijving || null,
                d.conflictMetDienst || null,
                d.status || "upcoming",
                d.kalender || "primary"
            ]);
            count++;
        } catch (e) {
            console.error("Fout personal event", e);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} personal events`);
}

async function migrateSalary() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/salary/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen salary gevonden");

    console.log(`Migrating ${data.length} salary records...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO salary (
                    user_id, periode, jaar, maand, aantal_diensten, uurloon_ort, basis_loon,
                    amt_zeerintensief, toeslag_balansvlf, ort_totaal, extra_uren_bedrag, toeslag_vakatie_uren,
                    reiskosten, eenmalig_totaal, bruto_betaling, pensioenpremie, loonheffing_schat, netto_prognose,
                    ort_detail, eenmalig_detail
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
                ON CONFLICT (user_id, periode) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.periode || "",
                d.jaar || 2026,
                d.maand || 1,
                d.aantalDiensten || 0,
                d.uurloonOrt || 0,
                d.basisLoon || 0,
                d.amtZeerintensief || 0,
                d.toeslagBalansvlf || 0,
                d.ortTotaal || 0,
                d.extraUrenBedrag || 0,
                d.toeslagVakatieUren || 0,
                d.reiskosten || 0,
                d.eenmaligTotaal || 0,
                d.brutoBetaling || 0,
                d.pensioenpremie || 0,
                d.loonheffingSchat || 0,
                d.nettoPrognose || 0,
                d.ortDetail ? JSON.stringify(d.ortDetail) : null,
                d.eenmaligDetail ? JSON.stringify(d.eenmaligDetail) : null
            ]);
            count++;
        } catch (e) {
            console.error("Fout salary", e);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} salary records`);
}

async function migrateEmails() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/emails/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen emails gevonden");

    console.log(`Migrating ${data.length} emails...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO emails (
                    user_id, gmail_id, thread_id, from_addr, to_addr, cc, bcc, subject,
                    snippet, datum, ontvangen, is_gelezen, is_ster, is_verwijderd, is_draft,
                    label_ids, categorie, heeft_bijlagen, bijlagen_count, search_text
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
                ON CONFLICT (user_id, gmail_id) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.gmailId || "",
                d.threadId || "",
                d.from || "",
                d.to || "",
                d.cc || null,
                d.bcc || null,
                d.subject || "",
                d.snippet || "",
                d.datum || new Date().toISOString(),
                d.ontvangen || 0,
                Boolean(d.isGelezen),
                Boolean(d.isSter),
                Boolean(d.isVerwijderd),
                Boolean(d.isDraft),
                d.labelIds || [],
                d.categorie || null,
                Boolean(d.heeftBijlagen),
                d.bijlagenCount || 0,
                d.searchText || ""
            ]);
            count++;
        } catch (e) {
            console.error("Fout email", e);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} emails`);
}

async function migrateNotes() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/notes/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen notes gevonden");

    console.log(`Migrating ${data.length} notes...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO notes (
                    user_id, titel, inhoud, tags, kleur, is_pinned, is_archived,
                    deadline, linked_event_id, prioriteit, triage_flag, aangemaakt, gewijzigd
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.titel || null,
                d.inhoud || "",
                d.tags || [],
                d.kleur || null,
                Boolean(d.isPinned),
                Boolean(d.isArchived),
                d.deadline || null,
                d.linkedEventId || null,
                d.prioriteit || null,
                Boolean(d.triageFlag),
                d.aangemaakt || new Date().toISOString(),
                d.gewijzigd || new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            console.error("Fout note", e);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} notes`);
}

async function migrateDevices() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/devices/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen devices gevonden");

    console.log(`Migrating ${data.length} devices...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO devices (
                    id, name, device_type, ip_address, current_state, status, last_seen, commissioned_at, manufacturer, model
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
                ON CONFLICT (id) DO NOTHING
            `, [
                d._id,
                d.name || "Device",
                d.deviceType || "wiz_lamp",
                d.ipAddress || null,
                d.currentState || '{"on": false, "brightness": 100, "color_temp": 2700}',
                d.status || "offline",
                d.lastSeen || null,
                d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString(),
                d.manufacturer || "WiZ",
                d.model || null
            ]);
            count++;
        } catch (e) {
            console.error("Fout device", e.message);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} devices`);
}

async function main() {
    try {
        await client.connect();
        console.log("Verbonden met database");
        
        await migrateTransactions();
        await migratePersonalEvents();
        await migrateSalary();
        await migrateEmails();
        await migrateNotes();
        await migrateDevices();
        
    } catch (e) {
        console.error("Fout:", e);
    } finally {
        await client.end();
    }
}

main();
