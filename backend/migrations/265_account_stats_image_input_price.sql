-- Historical account statistics rules inherit the text input rate until configured.
ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20,12);
