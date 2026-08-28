# frozen_string_literal: true

require "uri"

module ConnectIdentity
  module Configuration
    module_function

    def valid?
      return false unless SiteSetting.connect_identity_enabled

      secret = SiteSetting.connect_identity_shared_secret.to_s.strip
      base_url = SiteSetting.connect_identity_base_url.to_s.strip
      return false if secret.blank? || base_url.blank?

      uri = URI.parse(base_url)
      uri.is_a?(URI::HTTPS) && uri.host.present? && uri.userinfo.blank? &&
        uri.query.blank? && uri.fragment.blank?
    rescue URI::InvalidURIError
      false
    end
  end
end
