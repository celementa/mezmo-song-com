package store

import "testing"

// Клиентский лимит берётся из настроек, а не из константы: правка в
// /admin/settings должна действовать без пересборки.
func TestClientRegenerationLimitFromSettings(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_one_try")
	if err := SetSetting(ctx, db, SettingRegenerationsClient, "1"); err != nil {
		t.Fatalf("set setting: %v", err)
	}

	if _, err := IncrementRegeneration(ctx, db, "ord_one_try"); err != nil {
		t.Fatalf("first regeneration: %v", err)
	}
	if _, err := IncrementRegeneration(ctx, db, "ord_one_try"); err == nil ||
		err.Error() != "max_regenerations_reached" {
		t.Fatalf("second regeneration: got err %v, want max_regenerations_reached", err)
	}
}

// 0 в настройке означает «без ограничений».
func TestZeroLimitMeansUnlimited(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_unlimited")
	if err := SetSetting(ctx, db, SettingRegenerationsClient, "0"); err != nil {
		t.Fatalf("set setting: %v", err)
	}

	for i := 1; i <= MaxRegenerations+2; i++ {
		if _, err := IncrementRegeneration(ctx, db, "ord_unlimited"); err != nil {
			t.Fatalf("regeneration %d: %v", i, err)
		}
	}
}

// Тестовым заказам можно выставить конечный лимит, и он не задевает клиентские.
func TestInternalLimitIsSeparate(t *testing.T) {
	ctx, db := testDB(t)
	newTestOrder(t, ctx, db, "ord_blogger")
	markInternal(t, ctx, db, "ord_blogger")
	newTestOrder(t, ctx, db, "ord_buyer")

	if err := SetSetting(ctx, db, SettingRegenerationsInternal, "1"); err != nil {
		t.Fatalf("set internal limit: %v", err)
	}

	if _, err := IncrementRegeneration(ctx, db, "ord_blogger"); err != nil {
		t.Fatalf("blogger first: %v", err)
	}
	if _, err := IncrementRegeneration(ctx, db, "ord_blogger"); err == nil ||
		err.Error() != "max_regenerations_reached" {
		t.Fatalf("blogger past limit: got err %v, want max_regenerations_reached", err)
	}

	// Клиентский лимит не тронут — по умолчанию MaxRegenerations.
	for i := 1; i <= MaxRegenerations; i++ {
		if _, err := IncrementRegeneration(ctx, db, "ord_buyer"); err != nil {
			t.Fatalf("buyer regeneration %d: %v", i, err)
		}
	}
}

// Мусор в настройке не должен открывать безлимит: падаем на дефолт.
func TestBrokenSettingFallsBackToDefault(t *testing.T) {
	ctx, db := testDB(t)
	if err := SetSetting(ctx, db, SettingRegenerationsClient, "не число"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	client, internal := RegenerationLimits(ctx, db)
	if client != MaxRegenerations {
		t.Fatalf("client limit = %d, want %d", client, MaxRegenerations)
	}
	if internal != defaultRegenerationsInternal {
		t.Fatalf("internal limit = %d, want %d", internal, defaultRegenerationsInternal)
	}
}
