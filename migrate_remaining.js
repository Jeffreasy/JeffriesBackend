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

async function migrateAuditLogs() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/auditLogs/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen auditLogs gevonden");

    console.log(`Migrating ${data.length} auditLogs...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO audit_logs (
                    user_id, actor, source, action, entity, entity_id, status, summary, metadata, created_at
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.actor || "system",
                d.source || "convex",
                d.action || "unknown",
                d.entity || "unknown",
                d.entityId || null,
                d.status || "success",
                d.summary || "",
                d.metadata ? JSON.stringify(d.metadata) : null,
                d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            // Ignore errors
        }
    }
    console.log(`✅ Geïmporteerd: ${count} auditLogs`);
}

async function migrateLaventeCareDocuments() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/laventecareDocuments/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen laventecareDocuments gevonden");

    console.log(`Migrating ${data.length} laventecareDocuments...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO lc_documents (
                    user_id, document_key, titel, categorie, fase, versie, source_path, samenvatting, tags, created_at, updated_at
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
                ON CONFLICT (user_id, document_key) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.documentKey || String(d._creationTime),
                d.titel || "Document",
                d.categorie || "overig",
                d.fase || null,
                d.versie || "1.0",
                d.sourcePath || null,
                d.samenvatting || "",
                d.tags || [],
                d.createdAt || d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString(),
                d.updatedAt || new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            console.error("Fout document", e.message);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} laventecareDocuments`);
}

async function migrateLoonstroken() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/loonstroken/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen loonstroken gevonden");

    console.log(`Migrating ${data.length} loonstroken...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO loonstroken (
                    user_id, jaar, periode, periode_label, type, netto, bruto_betaling, bruto_inhouding,
                    salaris_basis, ort_totaal, ort_detail, amt_zeerintensief, pensioenpremie, loonheffing,
                    reiskosten, vakantietoeslag, eju_bedrag, toeslag_balansvlf, extra_uren_bedrag, schaalnummer,
                    trede, parttime_factor, uurloon, componenten, cumulatieven, geimporteerd_op
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26)
                ON CONFLICT (user_id, jaar, periode) DO NOTHING
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.jaar || 2026,
                d.periode || 1,
                d.periodeLabel || "1",
                d.type || "loonstrook",
                d.netto || 0,
                d.brutoBetaling || 0,
                d.brutoInhouding || 0,
                d.salarisBasis || 0,
                d.ortTotaal || 0,
                d.ortDetail ? JSON.stringify(d.ortDetail) : '[]',
                d.amtZeerintensief || null,
                d.pensioenpremie || null,
                d.loonheffing || null,
                d.reiskosten || null,
                d.vakantietoeslag || null,
                d.ejuBedrag || null,
                d.toeslagBalansvlf || null,
                d.extraUrenBedrag || null,
                d.schaalnummer || '?',
                d.trede || '?',
                d.parttimeFactor || 0,
                d.uurloon || null,
                d.componenten ? JSON.stringify(d.componenten) : '[]',
                d.cumulatieven ? JSON.stringify(d.cumulatieven) : null,
                d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            console.error("Fout loonstrook", e.message);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} loonstroken`);
}

async function migrateLaventeCareProjects() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/laventecareProjects/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen laventecareProjects gevonden");

    console.log(`Migrating ${data.length} laventecareProjects...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO lc_projects (
                    user_id, naam, fase, status, waarde_indicatie, start_datum, deadline, samenvatting, created_at, updated_at
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.naam || "Project",
                d.fase || "initiatie",
                d.status || "actief",
                d.waardeIndicatie || null,
                d.startDatum || null,
                d.deadline || null,
                d.samenvatting || null,
                d.createdAt || d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString(),
                d.updatedAt || new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            console.error("Fout project", e.message);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} laventecareProjects`);
}

async function migrateHabits() {
    const path = "/Projecten/JeffriesHomeapp/tmp_convex/export/habits/documents.jsonl";
    const data = await loadJsonl(path);
    if (!data.length) return console.log("Geen habits gevonden");

    console.log(`Migrating ${data.length} habits...`);
    let count = 0;
    
    for (const d of data) {
        try {
            await client.query(`
                INSERT INTO habits (
                    user_id, naam, emoji, type, beschrijving, frequentie, is_kwantitatief, xp_per_voltooiing, moeilijkheid,
                    huidige_streak, langste_streak, totaal_voltooid, totaal_xp, volgorde, is_actief, is_pauze, aangemaakt, gewijzigd
                ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
            `, [
                d.userId || "user_3Ax561ZvuSkGtWpKFooeY65HNtY",
                d.naam || "Habit",
                d.emoji || "⭐",
                d.type || "dagelijks",
                d.beschrijving || null,
                d.frequentie || "dagelijks",
                Boolean(d.isKwantitatief),
                d.xpPerVoltooiing || 10,
                d.moeilijkheid || "normaal",
                d.huidigeStreak || 0,
                d.langsteStreak || 0,
                d.totaalVoltooid || 0,
                d.totaalXp || 0,
                d.volgorde || 0,
                d.isActief !== false,
                Boolean(d.isPauze),
                d.aangemaakt || d._creationTime ? new Date(d._creationTime).toISOString() : new Date().toISOString(),
                d.gewijzigd || new Date().toISOString()
            ]);
            count++;
        } catch (e) {
            console.error("Fout habit", e.message);
        }
    }
    console.log(`✅ Geïmporteerd: ${count} habits`);
}

async function main() {
    try {
        await client.connect();
        console.log("Verbonden met database voor remaining tables");
        
        await migrateAuditLogs();
        await migrateLaventeCareDocuments();
        await migrateLoonstroken();
        await migrateLaventeCareProjects();
        await migrateHabits();
        
    } catch (e) {
        console.error("Fout:", e);
    } finally {
        await client.end();
    }
}

main();
