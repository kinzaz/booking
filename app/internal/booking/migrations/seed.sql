INSERT INTO events (id,title,category,venue,description,starts_at,capacity,remaining) VALUES
('night-jazz','Джаз после заката','Музыка','Клуб «Маяк»','Камерный концерт с живой импровизацией, контрабасом и саксофоном.',CURRENT_DATE + interval '7 days 19 hours',80,80),
('city-layers','Город в деталях','Экскурсия','Сбор у главного входа в парк','Пешая прогулка о том, как архитектура и повседневная жизнь меняли город.',CURRENT_DATE + interval '10 days 13 hours',24,24),
('print-studio','Мастерская линогравюры','Мастер-класс','Студия «Оттиск»','Создайте собственный рисунок, вырежьте форму и сделайте первый оттиск.',CURRENT_DATE + interval '12 days 15 hours',12,12),
('last-seat','Открытая репетиция','Театр','Малая сцена','Один билет на репетицию. Подходит для проверки конкуренции за последнее место.',CURRENT_DATE + interval '15 days 18 hours',1,1)
ON CONFLICT (id) DO NOTHING;
