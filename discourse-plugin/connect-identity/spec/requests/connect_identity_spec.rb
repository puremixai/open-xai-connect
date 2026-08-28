# frozen_string_literal: true

require "rails_helper"
require "openssl"

RSpec.describe "Connect identity bridge", type: :request do
  let(:secret) { "shared-secret" }
  let(:timestamp) { Time.now.to_i }
  let(:nonce) { "spec-nonce-1" }
  let(:body) { "" }

  def signed_headers(method, path, payload = "")
    canonical = [method, path, timestamp, nonce, payload].join("\n")
    {
      "HTTP_X_CONNECT_TIMESTAMP" => timestamp.to_s,
      "HTTP_X_CONNECT_NONCE" => nonce,
      "HTTP_X_CONNECT_SIGNATURE" => OpenSSL::HMAC.hexdigest("SHA256", secret, canonical)
    }
  end

  before do
    SiteSetting.connect_identity_enabled = true
    SiteSetting.connect_identity_shared_secret = secret
  end

  it "returns only the approved user fields" do
    user = Fabricate(:user, username: "alice", name: "Alice", email: "alice@example.com", trust_level: 1)
    get "/connect/identity/users/#{user.id}", headers: signed_headers("GET", "/connect/identity/users/#{user.id}")
    expect(response).to have_http_status(:ok)
    json = response.parsed_body
    expect(json.keys).to contain_exactly(
      "discourse_id", "username", "name", "email", "avatar_url", "trust_level",
      "active", "silenced", "suspended", "connect_reviewer", "connect_admin"
    )
    expect(json["email"]).to eq("alice@example.com")
    expect(json).not_to include("groups", "api_key")
  end

  it "rejects a replayed nonce" do
    user = Fabricate(:user)
    headers = signed_headers("GET", "/connect/identity/users/#{user.id}")
    get "/connect/identity/users/#{user.id}", headers: headers
    expect(response).to have_http_status(:ok)
    get "/connect/identity/users/#{user.id}", headers: headers
    expect(response).to have_http_status(:unauthorized)
  end
end
