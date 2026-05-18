-- Seed WiZ devices from Convex export
INSERT INTO devices (id, name, device_type, manufacturer, model, ip_address, status, current_state, last_seen, commissioned_at, matter_node_id, matter_endpoint_id)
VALUES
  (gen_random_uuid(), 'Lamp 1', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.28',  'online', '{"on":false,"brightness":40,"color_temp":2500,"r":146,"g":34,"b":237}', '2026-05-17T21:27:14.889Z', '2026-03-20T01:00:07.385Z', 0, 0),
  (gen_random_uuid(), 'Lamp 2', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.88',  'online', '{"on":false,"brightness":40,"color_temp":2500,"r":100,"g":0,"b":180}',  '2026-05-17T21:27:14.987Z', '2026-03-20T01:00:07.799Z', 0, 0),
  (gen_random_uuid(), 'Lamp 3', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.138', 'online', '{"on":false,"brightness":40,"color_temp":2500,"r":100,"g":0,"b":180}',  '2026-05-17T21:27:14.473Z', '2026-03-20T01:00:08.220Z', 0, 0),
  (gen_random_uuid(), 'Lamp 4', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.161', 'online', '{"on":false,"brightness":40,"color_temp":2500,"r":100,"g":0,"b":180}',  '2026-05-17T21:27:14.577Z', '2026-03-20T01:00:08.530Z', 0, 0),
  (gen_random_uuid(), 'Lamp 5', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.210', 'online', '{"on":false,"brightness":40,"color_temp":2500,"r":100,"g":0,"b":180}',  '2026-05-17T21:27:14.681Z', '2026-03-20T01:00:09.532Z', 0, 0),
  (gen_random_uuid(), 'Lamp 6', 'color_light', 'WiZ', 'GU10 Color', '192.168.1.236', 'online', '{"on":false,"brightness":40,"color_temp":2500,"r":100,"g":0,"b":180}',  '2026-05-17T21:27:14.785Z', '2026-03-20T01:00:09.944Z', 0, 0);
