import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';

const date = new Intl.DateTimeFormat('ru-RU', { dateStyle: 'long', timeStyle: 'short' });
const statuses = { active: 'Активна', cancelled: 'Отменена', expired: 'Истекла' };
const pendingKey = 'tickets.pending';

async function request(path, options = {}) {
  const response = await fetch('/api' + path, {
    ...options,
    signal: AbortSignal.timeout(8000),
  });
  const data = await response.json().catch(() => null);
  if (!response.ok || !data) {
    throw Object.assign(new Error(data?.error || 'Сервис временно недоступен'), { status: response.status });
  }
  return data;
}

function errorText(error) {
  return error.name === 'TimeoutError' || error instanceof TypeError
    ? 'Нет ответа от сервера. Повторите запрос.'
    : error.message;
}

function App() {
  const [events, setEvents] = useState(null);
  const [catalogError, setCatalogError] = useState('');
  const [selection, setSelection] = useState(null);
  const [quantity, setQuantity] = useState(1);
  const [bookingError, setBookingError] = useState('');
  const [lookupError, setLookupError] = useState('');
  const [id, setId] = useState('');
  const [reservation, setReservation] = useState(null);
  const [busy, setBusy] = useState(false);

  async function refresh() {
    try {
      setEvents(await request('/events'));
      setCatalogError('');
    } catch (error) {
      setCatalogError(errorText(error));
    }
  }

  useEffect(() => { void refresh(); }, []);

  function select(event) {
    setSelection(event);
    setQuantity(1);
    setBookingError('');
  }

  async function create(event) {
    event.preventDefault();
    setBusy(true);
    setBookingError('');
    const input = { event_id: selection.id, quantity };
    const body = JSON.stringify(input);
    try {
      const previous = JSON.parse(sessionStorage.getItem(pendingKey) || 'null');
      const key = previous?.body === body ? previous.key : crypto.randomUUID();
      sessionStorage.setItem(pendingKey, JSON.stringify({ key, body }));
      const created = await request('/bookings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Idempotency-Key': key },
        body,
      });
      sessionStorage.removeItem(pendingKey);
      setId(created.id);
      setReservation(created);
      setLookupError('');
      setSelection(null);
      document.getElementById('lookup').scrollIntoView();
      void refresh();
    } catch (error) {
      setBookingError(errorText(error));
      if (error.status === 409) void refresh();
    } finally {
      setBusy(false);
    }
  }

  async function lookup(method) {
    setBusy(true);
    setLookupError('');
    try {
      setReservation(await request('/bookings/' + encodeURIComponent(id.trim()), { method }));
      if (method === 'DELETE') void refresh();
    } catch (error) {
      setLookupError(errorText(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <header className="top"><div className="wrap nav">
        <a className="brand" href="#catalog">Места<span>.</span></a>
        <a href="#lookup">Найти бронь ↗</a>
      </div></header>
      <main className="wrap">
        <section className="intro">
          <h1>Билеты на<br />мероприятия<span>.</span></h1>
          <p>Выберите событие и забронируйте места.</p>
        </section>
        <section id="catalog" aria-labelledby="catalog-title">
          <div className="section-head">
            <h2 id="catalog-title">Афиша</h2>
            <button type="button" className="secondary" onClick={refresh}>Обновить места</button>
          </div>
          {catalogError && <p role="alert">{catalogError}</p>}
          {!events && !catalogError && <p role="status">Загружаем афишу…</p>}
          {events?.length === 0 && <p>Пока нет предстоящих мероприятий.</p>}
          <div className="grid">
            {events?.map((event, index) => (
              <article key={event.id} className="card">
                <div className={'category tone-' + index % 4}>{event.category}</div>
                <div className="card-body">
                  <p className="date">{date.format(new Date(event.starts_at))}</p>
                  <h3>{event.title}</h3>
                  <p>{event.description}</p>
                  <p className="venue">{event.venue}</p>
                  <div className="card-actions">
                    <span>{event.remaining > 0 ? 'Свободных мест: ' + event.remaining : 'Мест нет'}</span>
                    <button disabled={event.remaining === 0 || busy} onClick={() => select(event)}>Выбрать</button>
                  </div>
                  {selection?.id === event.id && (
                    <form className="booking-form" onSubmit={create}>
                      <label>Количество билетов
                        <input type="number" min="1" max={Math.max(1, Math.min(8, event.remaining))}
                          required value={quantity} onChange={e => setQuantity(Number(e.target.value))} />
                      </label>
                      <div className="form-actions">
                        <button disabled={busy}>Забронировать</button>
                        <button type="button" className="secondary" disabled={busy} onClick={() => setSelection(null)}>Закрыть</button>
                      </div>
                      {bookingError && <p role="alert">{bookingError}</p>}
                    </form>
                  )}
                </div>
              </article>
            ))}
          </div>
        </section>
        <section id="lookup" className="lookup" aria-labelledby="lookup-title">
          <h2 id="lookup-title">Моя бронь</h2>
          <form onSubmit={event => { event.preventDefault(); void lookup('GET'); }}>
            <label htmlFor="booking-id">Номер брони</label>
            <div className="form-actions">
              <input id="booking-id" autoComplete="off" required readOnly={busy} value={id}
                placeholder="Введите номер брони"
                onChange={event => { setId(event.target.value); setReservation(null); setLookupError(''); }} />
              <button disabled={busy}>Проверить статус</button>
            </div>
          </form>
          {lookupError && <p role="alert">{lookupError}</p>}
          {reservation && (
            <div className="reservation" aria-live="polite">
              <strong>{statuses[reservation.status]}</strong>
              <p className="reservation-id">{reservation.id}</p>
              <p>{events?.find(event => event.id === reservation.event_id)?.title || reservation.event_id} · Билетов: {reservation.quantity}</p>
              {reservation.status === 'active' && <p>Действует до {date.format(new Date(reservation.expires_at))}</p>}
              {reservation.confirmation_at && <p>Подтверждение обработано в тестовом режиме.</p>}
              {reservation.status === 'active' && <button className="secondary" disabled={busy} onClick={() => lookup('DELETE')}>Отменить бронь</button>}
            </div>
          )}
        </section>
      </main>
    </>
  );
}

createRoot(document.getElementById('root')).render(<App />);
