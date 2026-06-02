const fs = require('fs');
const path = require('path');
const { Client } = require('pg');

const connectionString = "postgresql://jeffries_db_user:bAaNLTs64wOBpep7KKEMXKZpjS5dOhPA@dpg-d8f8e4mgvqtc7391t0dg-a.frankfurt-postgres.render.com/jeffries_db";

const client = new Client({
    connectionString: connectionString,
    ssl: {
        rejectUnauthorized: false
    }
});

async function run() {
    try {
        await client.connect();
        console.log("Connected to Render PostgreSQL database!");

        // Reset database schema to run migrations fresh
        console.log("Resetting public schema...");
        await client.query("DROP SCHEMA public CASCADE; CREATE SCHEMA public;");
        console.log("Schema reset complete.");

        const migrationsDir = path.join(__dirname, 'backend', 'migrations');
        const files = fs.readdirSync(migrationsDir)
            .filter(f => f.endsWith('.up.sql'))
            .sort();

        for (const file of files) {
            console.log(`Running migration: ${file}...`);
            const filePath = path.join(migrationsDir, file);
            const sql = fs.readFileSync(filePath, 'utf8');
            
            // Execute SQL statements
            await client.query(sql);
            console.log(`✅ Successfully applied ${file}`);
        }

        console.log("\n🎉 All migrations applied successfully!");
    } catch (err) {
        console.error("Migration failed:", err);
    } finally {
        await client.end();
    }
}

run();
