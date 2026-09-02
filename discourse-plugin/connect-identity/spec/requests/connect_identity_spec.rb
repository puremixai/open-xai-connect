# frozen_string_literal: true

require "rails_helper"
require "openssl"

RSpec.describe "Connect identity bridge", type: :request do
  let(:secret) { "shared-secret" }
  let(:timestamp) { Time.now.to_i }
  let(:nonce) { "spec-nonce-1" }
  let(:body) { "" }

  def signed_headers(method, path, payload = "", timestamp_value: timestamp, nonce_value: nonce, secret_value: secret)
    canonical = [method, path, timestamp_value, nonce_value, payload].join("\n")
    {
      "HTTP_X_CONNECT_TIMESTAMP" => timestamp_value.to_s,
      "HTTP_X_CONNECT_NONCE" => nonce_value,
      "HTTP_X_CONNECT_SIGNATURE" => OpenSSL::HMAC.hexdigest("SHA256", secret_value, canonical)
    }
  end

  def level_progress_path(user)
    "/connect/identity/users/#{user.id}/level-progress"
  end

  def requirement_map(json)
    json.fetch("requirements").index_by { |item| item.fetch("key") }
  end

  def blocking_condition_map(json)
    json.fetch("blocking_conditions").index_by { |item| item.fetch("key") }
  end

  def stub_tl3_requirements(values = {})
    defaults = {
      time_period: 100,
      days_visited: 40,
      min_days_visited: 50,
      num_topics_replied_to: 9,
      min_topics_replied_to: 10,
      topics_viewed: 70,
      min_topics_viewed: 80,
      posts_read: 190,
      min_posts_read: 200,
      topics_viewed_all_time: 250,
      min_topics_viewed_all_time: 300,
      posts_read_all_time: 800,
      min_posts_read_all_time: 900,
      num_likes_given: 25,
      min_likes_given: 30,
      num_likes_received: 19,
      min_likes_received: 20,
      num_likes_received_days: 6,
      min_likes_received_days: 7,
      num_likes_received_users: 4,
      min_likes_received_users: 5,
      num_flagged_posts: 1,
      max_flagged_posts: 0,
      num_flagged_by_users: 1,
      max_flagged_by_users: 0,
      penalty_counts: TrustLevel3Requirements::PenaltyCounts.new(
        Fabricate(:user),
        { "silence_count" => 1, "suspend_count" => 0 }
      )
    }

    stubbed = defaults.merge(values)
    stubbed.each do |method_name, return_value|
      TrustLevel3Requirements.any_instance.stubs(method_name).returns(return_value)
    end
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

  it "returns target level one requirements for a new user" do
    freeze_time do
      SiteSetting.tl1_requires_topics_entered = 5
      SiteSetting.tl1_requires_read_posts = 30
      SiteSetting.tl1_requires_time_spent_mins = 10

      user = Fabricate(:user, trust_level: 0, created_at: 30.minutes.ago)
      user.user_stat.update!(topics_entered: 1, posts_read_count: 2, time_read: 60)

      path = level_progress_path(user)
      get path, headers: signed_headers("GET", path)

      expect(response).to have_http_status(:ok)
      json = response.parsed_body
      expect(json.fetch("current_level").fetch("id")).to eq(0)
      expect(json.fetch("next_level").fetch("id")).to eq(1)
      expect(json.fetch("promotion_mode")).to eq("automatic")
      expect(json.fetch("requirements_met")).to eq(false)

      requirements = requirement_map(json)
      expect(requirements.keys).to contain_exactly(
        "topics_entered",
        "posts_read",
        "time_read_minutes",
        "account_age_minutes"
      )
      expect(requirements.fetch("topics_entered")).to include(
        "group" => "activity",
        "scope" => "account_lifetime",
        "period_days" => nil,
        "current" => 1,
        "target" => 5,
        "operator" => "at_least",
        "unit" => "count",
        "met" => false
      )
      expect(requirements.fetch("account_age_minutes")).to include(
        "current" => 30,
        "target" => 10,
        "operator" => "at_least",
        "unit" => "minutes",
        "met" => true
      )
    end
  end

  it "returns target level two requirements for a basic user" do
    freeze_time do
      SiteSetting.tl2_requires_topics_entered = 10
      SiteSetting.tl2_requires_read_posts = 50
      SiteSetting.tl2_requires_time_spent_mins = 90
      SiteSetting.tl2_requires_days_visited = 5
      SiteSetting.tl2_requires_likes_received = 3
      SiteSetting.tl2_requires_likes_given = 2
      SiteSetting.tl2_requires_topic_reply_count = 2

      user = Fabricate(:user, trust_level: 1, created_at: 2.hours.ago)
      user.user_stat.update!(
        topics_entered: 20,
        posts_read_count: 40,
        time_read: 95 * 60,
        days_visited: 4,
        likes_received: 1,
        likes_given: 2
      )

      replied_topic = Fabricate(:topic)
      Fabricate(:post, topic: replied_topic, user: user, post_number: 2)

      path = level_progress_path(user)
      get path, headers: signed_headers("GET", path)

      expect(response).to have_http_status(:ok)
      json = response.parsed_body
      expect(json.fetch("current_level").fetch("id")).to eq(1)
      expect(json.fetch("next_level").fetch("id")).to eq(2)
      expect(json.fetch("promotion_mode")).to eq("automatic")
      expect(json.fetch("requirements_met")).to eq(false)

      requirements = requirement_map(json)
      expect(requirements.fetch("topics_entered")).to include("current" => 20, "target" => 10, "met" => true)
      expect(requirements.fetch("posts_read")).to include("current" => 40, "target" => 50, "met" => false)
      expect(requirements.fetch("time_read_minutes")).to include("current" => 95, "target" => 90, "met" => true)
      expect(requirements.fetch("days_visited")).to include(
        "group" => "activity",
        "current" => 4,
        "target" => 5,
        "unit" => "days",
        "met" => false
      )
      expect(requirements.fetch("likes_received")).to include(
        "group" => "interaction",
        "current" => 1,
        "target" => 3,
        "met" => false
      )
      expect(requirements.fetch("likes_given")).to include("current" => 2, "target" => 2, "met" => true)
      expect(requirements.fetch("num_topics_replied_to")).to include(
        "group" => "interaction",
        "current" => 1,
        "target" => 2,
        "operator" => "at_least",
        "unit" => "count",
        "met" => false
      )
    end
  end

  it "returns target level three requirements from the trust level rule object" do
    user = Fabricate(:user, trust_level: 2)
    stub_tl3_requirements

    path = level_progress_path(user)
    get path, headers: signed_headers("GET", path)

    expect(response).to have_http_status(:ok)
    json = response.parsed_body
    expect(json.fetch("current_level").fetch("id")).to eq(2)
    expect(json.fetch("next_level").fetch("id")).to eq(3)
    expect(json.fetch("promotion_mode")).to eq("automatic")
    expect(json.fetch("requirements_met")).to eq(false)

    requirements = requirement_map(json)
    expect(requirements.fetch("days_visited")).to include(
      "scope" => "rolling_period",
      "period_days" => 100,
      "current" => 40,
      "target" => 50,
      "operator" => "at_least",
      "unit" => "days",
      "met" => false
    )
    expect(requirements.fetch("topics_viewed_all_time")).to include(
      "scope" => "all_time",
      "period_days" => nil,
      "current" => 250,
      "target" => 300,
      "met" => false
    )
    expect(requirements.fetch("num_flagged_posts")).to include(
      "group" => "compliance",
      "scope" => "rolling_period",
      "period_days" => 100,
      "current" => 1,
      "target" => 0,
      "operator" => "at_most",
      "unit" => "count",
      "met" => false
    )
    expect(requirements.fetch("num_flagged_by_users")).to include(
      "operator" => "at_most",
      "current" => 1,
      "target" => 0,
      "met" => false
    )

    blocking_conditions = blocking_condition_map(json)
    expect(blocking_conditions.fetch("not_silenced")).to include("met" => true)
    expect(blocking_conditions.fetch("not_suspended")).to include("met" => true)
    expect(blocking_conditions.fetch("no_recent_penalties")).to include("met" => false)
  end

  it "returns manual promotion metadata for level three users" do
    user = Fabricate(:user, trust_level: 3)

    path = level_progress_path(user)
    get path, headers: signed_headers("GET", path)

    expect(response).to have_http_status(:ok)
    json = response.parsed_body
    expect(json.fetch("current_level").fetch("id")).to eq(3)
    expect(json.fetch("next_level").fetch("id")).to eq(4)
    expect(json.fetch("promotion_mode")).to eq("manual")
    expect(json.fetch("requirements_met")).to be_nil
    expect(json.fetch("requirements")).to eq([])
    expect(json.fetch("blocking_conditions")).to eq([])
  end

  it "returns no next level for level four users" do
    user = Fabricate(:user, trust_level: 4)

    path = level_progress_path(user)
    get path, headers: signed_headers("GET", path)

    expect(response).to have_http_status(:ok)
    json = response.parsed_body
    expect(json.fetch("current_level").fetch("id")).to eq(4)
    expect(json.fetch("next_level")).to be_nil
    expect(json.fetch("promotion_mode")).to eq("none")
    expect(json.fetch("requirements_met")).to be_nil
    expect(json.fetch("requirements")).to eq([])
    expect(json.fetch("blocking_conditions")).to eq([])
  end

  it "returns locked promotion metadata with a safe blocker" do
    freeze_time do
      SiteSetting.tl2_requires_topics_entered = 1
      SiteSetting.tl2_requires_read_posts = 1
      SiteSetting.tl2_requires_time_spent_mins = 1
      SiteSetting.tl2_requires_days_visited = 1
      SiteSetting.tl2_requires_likes_received = 1
      SiteSetting.tl2_requires_likes_given = 1
      SiteSetting.tl2_requires_topic_reply_count = 1

      user = Fabricate(:user, trust_level: 1, manual_locked_trust_level: 1, created_at: 2.hours.ago)
      user.user_stat.update!(
        topics_entered: 0,
        posts_read_count: 0,
        time_read: 0,
        days_visited: 0,
        likes_received: 0,
        likes_given: 0
      )

      path = level_progress_path(user)
      get path, headers: signed_headers("GET", path)

      expect(response).to have_http_status(:ok)
      json = response.parsed_body
      expect(json.fetch("promotion_mode")).to eq("locked")
      expect(json.fetch("requirements_met")).to be_nil
      expect(json.fetch("next_level").fetch("id")).to eq(2)
      expect(blocking_condition_map(json).fetch("not_manually_locked")).to include("met" => false)
    end
  end

  it "returns only the approved level progress fields" do
    freeze_time do
      SiteSetting.tl1_requires_topics_entered = 1
      SiteSetting.tl1_requires_read_posts = 1
      SiteSetting.tl1_requires_time_spent_mins = 1

      user = Fabricate(:user, trust_level: 0, created_at: 2.minutes.ago)
      user.user_stat.update!(topics_entered: 0, posts_read_count: 0, time_read: 0)

      path = level_progress_path(user)
      get path, headers: signed_headers("GET", path)

      expect(response).to have_http_status(:ok)
      json = response.parsed_body
      expect(json.keys).to contain_exactly(
        "schema_version",
        "discourse_id",
        "current_level",
        "next_level",
        "promotion_mode",
        "requirements_met",
        "requirements",
        "blocking_conditions",
        "generated_at"
      )
      expect(json).not_to include("groups", "api_key", "email", "username", "avatar_url", "name")
      expect(json.fetch("current_level").keys).to contain_exactly("id", "key", "label")
      expect(json.fetch("next_level").keys).to contain_exactly("id", "key", "label")
      json.fetch("requirements").each do |item|
        expect(item.keys).to contain_exactly(
          "key",
          "label",
          "group",
          "scope",
          "period_days",
          "current",
          "target",
          "operator",
          "unit",
          "met"
        )
      end
    end
  end

  it "rejects an invalid level progress signature" do
    user = Fabricate(:user)
    path = level_progress_path(user)
    headers = signed_headers("GET", path)
    headers["HTTP_X_CONNECT_SIGNATURE"] = "0" * 64

    get path, headers: headers

    expect(response).to have_http_status(:unauthorized)
  end

  it "rejects a replayed nonce on the level progress route" do
    user = Fabricate(:user)
    path = level_progress_path(user)
    headers = signed_headers("GET", path)

    get path, headers: headers
    expect(response).to have_http_status(:ok)

    get path, headers: headers
    expect(response).to have_http_status(:unauthorized)
  end
end
