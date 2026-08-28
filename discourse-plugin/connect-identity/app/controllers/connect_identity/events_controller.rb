# frozen_string_literal: true

class ConnectIdentity::EventsController < ConnectIdentity::BaseController
  def create
    payload = JSON.parse(request.raw_post)
    user_id = Integer(payload.fetch("user_id"), 10)
    event_id = payload.fetch("event_id").to_s
    return head :bad_request if event_id.blank? || event_id.length > 128

    user = User.find_by(id: user_id)
    return head :not_found if user.blank?

    ConnectIdentity::Publisher.publish(user)
    head :accepted
  rescue JSON::ParserError, KeyError, ArgumentError, TypeError
    head :bad_request
  end
end
