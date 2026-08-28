# frozen_string_literal: true

require "json"
require "net/http"
require "openssl"
require "securerandom"
require "uri"

module ConnectIdentity
  module Publisher
    module_function

    def enqueue(user)
      return unless SiteSetting.connect_identity_enabled
      return if user.blank?

      Jobs.enqueue(:connect_identity_publish_status, user_id: user.id)
    end

    def publish(user)
      return unless SiteSetting.connect_identity_enabled

      payload = {
        event_id: SecureRandom.uuid,
        user_id: user.id,
        event_type: "user_status_changed",
        occurred_at: Time.now.utc.iso8601,
        status: UserSerializer.new(user).status_fields
      }
      body = JSON.generate(payload)
      uri = URI.join(SiteSetting.connect_identity_base_url.to_s, "/connect/identity/events")
      timestamp = Time.now.to_i
      nonce = SecureRandom.hex(18)
      canonical = ["POST", uri.path, timestamp, nonce, body].join("\n")
      signature = OpenSSL::HMAC.hexdigest("SHA256", SiteSetting.connect_identity_shared_secret.to_s, canonical)

      request = Net::HTTP::Post.new(uri)
      request["Content-Type"] = "application/json"
      request["X-Connect-Timestamp"] = timestamp.to_s
      request["X-Connect-Nonce"] = nonce
      request["X-Connect-Signature"] = signature
      request.body = body
      http = Net::HTTP.new(uri.host, uri.port)
      http.use_ssl = uri.scheme == "https"
      http.open_timeout = 5
      http.read_timeout = 5
      http.request(request)
    end

    class UserSerializer
      def initialize(user)
        @user = user
      end

      def status_fields
        {
          discourse_id: @user.id,
          username: @user.username,
          name: @user.name.to_s,
          avatar_url: avatar_url,
          trust_level: @user.trust_level,
          active: @user.active?,
          silenced: @user.silenced?,
          suspended: @user.suspended?,
          connect_reviewer: in_groups?(SiteSetting.connect_identity_reviewer_groups),
          connect_admin: in_groups?(SiteSetting.connect_identity_admin_groups)
        }
      end

      private

      def avatar_url
        template = @user.avatar_template.to_s
        template.gsub("{size}", "240")
      end

      def in_groups?(setting)
        names = setting.to_s.split("|").map(&:strip).reject(&:blank?)
        return false if names.empty?

        @user.groups.any? { |group| names.include?(group.name) }
      end
    end
  end
end
