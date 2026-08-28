# frozen_string_literal: true

require "openssl"
require "digest"

module ConnectIdentity
  module SignedRequest
    HEADER_TIMESTAMP = "HTTP_X_CONNECT_TIMESTAMP"
    HEADER_NONCE = "HTTP_X_CONNECT_NONCE"
    HEADER_SIGNATURE = "HTTP_X_CONNECT_SIGNATURE"

    def verify_connect_request!
      secret = SiteSetting.connect_identity_shared_secret.to_s
      return head :service_unavailable if secret.blank?

      timestamp_raw = request.get_header(HEADER_TIMESTAMP).to_s
      nonce = request.get_header(HEADER_NONCE).to_s.strip
      signature = request.get_header(HEADER_SIGNATURE).to_s.strip
      timestamp = Integer(timestamp_raw, 10)
      window = SiteSetting.connect_identity_request_window_seconds.to_i
      return head :unauthorized if nonce.blank? || nonce.length > 128 || signature.blank?
      return head :unauthorized if (Time.now.to_i - timestamp).abs > window

      canonical = [
        request.request_method.upcase,
        request.path,
        timestamp,
        nonce,
        request.raw_post.to_s
      ].join("\n")
      expected = OpenSSL::HMAC.hexdigest("SHA256", secret, canonical)
      return head :unauthorized unless secure_compare_hex(expected, signature)

      nonce_key = "connect_identity:nonce:#{Digest::SHA256.hexdigest(nonce)}"
      # Discourse's Cache wrapper deliberately exposes only a small API and
      # does not support the ActiveSupport `unless_exist` option. Use the
      # namespaced Redis client for an atomic NX+EX write instead.
      accepted = Discourse.redis.set(nonce_key, "1", nx: true, ex: window)
      return head :unauthorized unless accepted
    rescue ArgumentError, TypeError
      head :unauthorized
    end

    private

    def secure_compare_hex(expected, provided)
      return false unless provided.match?(/\A[0-9a-fA-F]{64}\z/)

      ActiveSupport::SecurityUtils.secure_compare(expected.downcase, provided.downcase)
    end
  end
end
