// Package insales — минимальный клиент API InSales для интеграции с магазином
// Les Roses: прочитать заказ и список недавно изменённых заказов. Пишущих
// методов нет намеренно — магазин партнёра мы только читаем.
//
// Авторизация — Basic Auth парой «идентификатор / пароль» API-ключа магазина
// (Расширения → Разработчикам).
package insales

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// ErrNotFound — заказа нет (удалён или чужой id).
var ErrNotFound = errors.New("insales: not found")

type Client struct {
	BaseURL  string // https://myshop-xxx.myinsales.ru
	ID       string
	Password string
}

// Configured — заданы ли адрес и ключ; без них интеграция выключена.
func (c Client) Configured() bool {
	return c.BaseURL != "" && c.ID != "" && c.Password != ""
}

// Order — поля заказа, которые читает наш код, и ничего больше.
type Order struct {
	ID                int64       `json:"id"`
	Number            int64       `json:"number"`
	FinancialStatus   string      `json:"financial_status"`   // pending | paid
	FulfillmentStatus string      `json:"fulfillment_status"` // new, accepted, approved, dispatched, delivered, returning, returned, declined
	Client            OrderClient `json:"client"`
	OrderLines        []OrderLine `json:"order_lines"`
	PaidAt            string      `json:"paid_at"`
	UpdatedAt         string      `json:"updated_at"`
}

type OrderClient struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type OrderLine struct {
	VariantID int64   `json:"variant_id"`
	SKU       *string `json:"sku"`
	Title     string  `json:"title"`
	Quantity  int     `json:"quantity"`
	SalePrice float64 `json:"sale_price"`
}

// LineSKU — артикул строки; у большинства товаров магазина он не заполнен (null).
func (l OrderLine) LineSKU() string {
	if l.SKU == nil {
		return ""
	}
	return *l.SKU
}

// GetOrder читает заказ по внутреннему id InSales (не по номеру для людей).
func (c Client) GetOrder(ctx context.Context, id int64) (Order, error) {
	var o Order
	err := c.get(ctx, "/admin/orders/"+strconv.FormatInt(id, 10)+".json", nil, &o)
	return o, err
}

// OrdersUpdatedSince — заказы, изменённые после since, постранично (по 100).
// Нужен фоновой сверке: потерянный вебхук не должен означать потерянную песню.
func (c Client) OrdersUpdatedSince(ctx context.Context, since time.Time, page int) ([]Order, error) {
	q := url.Values{}
	q.Set("updated_since", since.Format(time.RFC3339))
	q.Set("per_page", "100")
	q.Set("page", strconv.Itoa(page))
	var out []Order
	err := c.get(ctx, "/admin/orders.json", q, &out)
	return out, err
}

func (c Client) get(ctx context.Context, path string, q url.Values, dst any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.ID, c.Password)
	req.Header.Set("Accept", "application/json")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("insales %s: HTTP %d: %.200s", path, res.StatusCode, body)
	}
	return json.Unmarshal(body, dst)
}
