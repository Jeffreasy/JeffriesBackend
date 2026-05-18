import json
import requests
import os

SCHEDULE_API_URL = "http://localhost:8000/api/v1/schedule/import"
DOCS_API_URL = "http://localhost:8000/api/v1/laventecare/documents/seed"
API_KEY = "homeapp-local-dev-2026-change-in-prod"

def load_jsonl(filepath):
    data = []
    with open(filepath, "r", encoding="utf-8") as f:
        for line in f:
            if line.strip():
                data.append(json.loads(line))
    return data

def import_schedule():
    schedule_path = r"C:\Users\jeffrey\Desktop\Projecten\JeffriesHomeapp\tmp_convex\export\schedule\documents.jsonl"
    if not os.path.exists(schedule_path):
        print(f"❌ Bestand '{schedule_path}' niet gevonden!")
        return

    data = load_jsonl(schedule_path)
    print(f"Gevonden: {len(data)} schedule documenten in Convex export.")
    
    rows = []
    user_id = "user_3Ax561ZvuSkGtWpKFooeY65HNtY" # Standaard fallback
    
    for d in data:
        if d.get("userId"):
            user_id = d["userId"]
            
        rows.append({
            "eventId": d.get("eventId", ""),
            "titel": d.get("titel", "Dienst"),
            "startDatum": d.get("startDatum", ""),
            "startTijd": d.get("startTijd", ""),
            "eindDatum": d.get("eindDatum", ""),
            "eindTijd": d.get("eindTijd", ""),
            "werktijd": d.get("werktijd", ""),
            "locatie": d.get("locatie", ""),
            "team": d.get("team", ""),
            "shiftType": d.get("shiftType", ""),
            "prioriteit": d.get("prioriteit", 0),
            "duur": float(d.get("duur", 0.0)),
            "weeknr": d.get("weeknr", ""),
            "dag": d.get("dag", ""),
            "status": d.get("status", ""),
            "beschrijving": d.get("beschrijving", ""),
            "heledag": d.get("heledag", False)
        })
        
    payload = {
        "userId": user_id,
        "fileName": "Convex Historie Export",
        "rows": rows
    }

    print("Versturen schedule naar Go Backend...")
    resp = requests.post(SCHEDULE_API_URL, json=payload, headers={"X-API-Key": API_KEY})
    if resp.status_code == 200:
        print("✅ Schedule Import succesvol:", resp.json())
    else:
        print("❌ Schedule Import mislukt:", resp.text)

def import_docs():
    docs_path = r"C:\Users\jeffrey\Desktop\Projecten\JeffriesHomeapp\tmp_convex\export\laventecareDocuments\documents.jsonl"
    if not os.path.exists(docs_path):
        print(f"❌ Bestand '{docs_path}' niet gevonden!")
        return

    data = load_jsonl(docs_path)
    print(f"Gevonden: {len(data)} LaventeCare documenten in Convex export.")
    
    docs_to_seed = []
    user_id = "user_3Ax561ZvuSkGtWpKFooeY65HNtY" # Standaard fallback
    
    for d in data:
        if d.get("userId"):
            user_id = d["userId"]
            
        # De seed handler verwacht een array van Document structs (of vergelijkbare JSON input)
        # We bouwen een basis object dat naar LaventeCareDocument mapt.
        docs_to_seed.append({
            "document_key": d.get("documentKey", ""),
            "titel": d.get("titel", ""),
            "samenvatting": d.get("samenvatting", ""),
            "categorie": d.get("categorie", ""),
            "fase": d.get("fase", ""),
            "versie": d.get("versie", ""),
            "source_path": d.get("sourcePath", ""),
            "tags": d.get("tags", []),
            "user_id": user_id,
        })
        
    payload = {
        "documents": docs_to_seed
    }

    print("Versturen LaventeCare documenten naar Go Backend...")
    # Seed endpoint is POST /seed, body: {"documents": [...]}
    resp = requests.post(DOCS_API_URL, json=payload, headers={"X-API-Key": API_KEY})
    if resp.status_code == 200:
        print("✅ Documents Seed succesvol:", resp.json())
    else:
        print("❌ Documents Seed mislukt:", resp.text)

if __name__ == "__main__":
    import_schedule()
    # import_docs() is nog niet actief voor de zekerheid, maar we doen nu eerst schedule!
    print("Klaar!")
