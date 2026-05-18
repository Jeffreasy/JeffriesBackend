-- Seed automations from Convex export
INSERT INTO automations (id, user_id, name, enabled, created_at, last_fired_at, group_name, trigger_config, action_config)
VALUES
  (gen_random_uuid(), 'user_3Ax561ZvuSkGtWpKFooeY65HNtY', 'Opstaan', true,
   '2026-03-22T04:04:29.085Z', '2026-04-26T08:01:03.771Z', NULL,
   '{"days":[0,1,2,3,4,5,6],"time":"10:00"}',
   '{"type":"scene","sceneId":"helder"}'),

  (gen_random_uuid(), 'user_3Ax561ZvuSkGtWpKFooeY65HNtY', '🚪 Vroeg — Vertrek (06:15)', true,
   '2026-03-14T21:06:00.852Z', '2026-04-24T04:16:10.104Z', 'dienst-wekker-vroeg',
   '{"triggerType":"schedule","shiftType":"Vroeg","time":"06:15"}',
   '{"type":"off"}'),

  (gen_random_uuid(), 'user_3Ax561ZvuSkGtWpKFooeY65HNtY', '☀️ Vroeg — Klaar (05:30)', true,
   '2026-03-14T21:06:00.852Z', '2026-04-24T03:31:30.474Z', 'dienst-wekker-vroeg',
   '{"triggerType":"schedule","shiftType":"Vroeg","time":"05:30"}',
   '{"type":"scene","sceneId":"helder"}'),

  (gen_random_uuid(), 'user_3Ax561ZvuSkGtWpKFooeY65HNtY', '🌅 Vroeg — Opstaan (05:00)', true,
   '2026-03-14T21:06:00.852Z', '2026-04-24T03:01:32.931Z', 'dienst-wekker-vroeg',
   '{"triggerType":"schedule","shiftType":"Vroeg","time":"05:00"}',
   '{"type":"scene","sceneId":"ochtend"}');
